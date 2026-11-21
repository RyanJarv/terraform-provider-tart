#!/bin/sh
# Run on Apple silicon with an already built provider and available Tart/Terraform binaries.
set -eu

if [ "$#" -ne 4 ]; then
    echo "Usage: $0 /abs/terraform /abs/tart /abs/provider-bin-dir /abs/ubuntu-arm64.iso" >&2
    exit 2
fi
terraform=$1
tart=$2
provider_dir=$3
installer=$4
for binary in "$terraform" "$tart" "$provider_dir/terraform-provider-tart"; do
    case "$binary" in /*) ;; *) echo 'Use absolute binary paths.' >&2; exit 2 ;; esac
    test -x "$binary"
done
case "$installer" in /*.iso) ;; *) echo 'Use an absolute ARM64 installer ISO path.' >&2; exit 2 ;; esac
test -f "$installer"

umask 077
fixture=$(mktemp -d "${TMPDIR:-/tmp}/tart-provider-lifecycle.XXXXXXXX")
echo "Disposable test store: $fixture"
export TART_HOME="$fixture/tart" TART_NO_AUTO_PRUNE=1 CHECKPOINT_DISABLE=1 TF_IN_AUTOMATION=1
export TF_CLI_CONFIG_FILE="$fixture/terraform.rc"
mkdir "$TART_HOME" "$fixture/create" "$fixture/import"
printf 'terraform-provider-tart disposable store v1\n' > "$TART_HOME/.terraform-tart-scratch"
cat > "$TF_CLI_CONFIG_FILE" <<CONFIG
provider_installation {
  dev_overrides {
    "registry.terraform.io/ryanjarv/tart" = "$provider_dir"
  }
}
CONFIG

for directory in "$fixture/create" "$fixture/import"; do
    cat > "$directory/provider.tf" <<CONFIG
terraform {
  required_providers {
    tart = {
      source = "ryanjarv/tart"
    }
  }
}
provider "tart" {
  tart_path = "$tart"
  tart_home = "$TART_HOME"
}
CONFIG
done

cat > "$fixture/create/main.tf" <<'CONFIG'
variable "cpus" {
  type    = number
  default = 2
}
variable "memory" {
  type    = number
  default = 2048
}
data "tart_vm" "base" {
  name = "fixture-base"
}
resource "tart_vm" "scratch" {
  name         = "fixture-clone"
  source_image = data.tart_vm.base.name
  disposable   = true
  cpu_count    = var.cpus
  memory_mib   = var.memory
}
output "base_os" {
  value = data.tart_vm.base.os
}
CONFIG
cat > "$fixture/import/main.tf" <<'CONFIG'
resource "tart_vm" "imported" {
  name       = "fixture-base"
  cpu_count  = 2
  memory_mib = 2048
}
CONFIG

tf() {
    "$terraform" "$@"
}

expect_failure() {
    pattern=$1
    shift
    if "$@" > "$fixture/expected-error.log" 2>&1; then
        echo "Expected failure for: $*" >&2
        exit 1
    fi
    if ! grep -q "$pattern" "$fixture/expected-error.log"; then
        cat "$fixture/expected-error.log" >&2
        exit 1
    fi
}

assert_size() {
    vm=$1
    cpu=$2
    memory=${3:-2048}
    "$tart" get "$vm" --format json > "$fixture/info.json"
    if [ "$(/usr/bin/plutil -extract CPU raw "$fixture/info.json")" != "$cpu" ] ||
        [ "$(/usr/bin/plutil -extract Memory raw "$fixture/info.json")" != "$memory" ]; then
        printf 'Expected %s to have %s CPUs and %s MiB. Observed:\n' "$vm" "$cpu" "$memory" >&2
        cat "$fixture/info.json" >&2
        exit 1
    fi
}

"$tart" create fixture-base --linux --disk-size 8
"$tart" set fixture-base --cpu 2 --memory 2048
cd "$fixture/create"
tf apply -auto-approve -no-color
test "$(tf output -raw base_os)" = linux
assert_size fixture-clone 2
tf plan -detailed-exitcode -no-color

"$tart" set fixture-clone --cpu 3
assert_size fixture-clone 3
status=0
tf plan -detailed-exitcode -no-color -out=restore.tfplan -var=cpus=2 || status=$?
test "$status" -eq 2
tf apply -no-color restore.tfplan
assert_size fixture-clone 2
tf plan -no-color -out=resize.tfplan -var=cpus=3 -var=memory=3072
tf apply -no-color resize.tfplan
assert_size fixture-clone 3 3072

"$tart" run --no-graphics --no-audio --no-clipboard --net-host \
    "--disk=$installer:ro" fixture-clone > "$fixture/run.log" 2>&1 &
running_pid=$!
trap 'kill -INT "$running_pid" 2>/dev/null || true' EXIT
tries=0
until "$tart" get fixture-clone --format json | grep -Eq '"State"[[:space:]]*:[[:space:]]*"running"'; do
    kill -0 "$running_pid" 2>/dev/null || { cat "$fixture/run.log" >&2; exit 1; }
    tries=$((tries + 1))
    test "$tries" -lt 20 || { cat "$fixture/run.log" >&2; exit 1; }
    sleep 1
done
expect_failure 'Tart resize blocked' tf plan -no-color -var=cpus=2
"$tart" stop fixture-clone --timeout 5
wait "$running_pid" || true
trap - EXIT

tf plan -destroy -out=destroy.tfplan -no-color
marker="$TART_HOME/vms/fixture-clone/.terraform-tart-owner"
mv "$marker" "$fixture/owner"
expect_failure 'Tart deletion blocked or failed' tf apply -no-color destroy.tfplan
test -f "$TART_HOME/vms/fixture-clone/disk.img"
mv "$fixture/owner" "$marker"
tf destroy -auto-approve -no-color
test ! -e "$TART_HOME/vms/fixture-clone"
assert_size fixture-base 2

cd "$fixture/import"
tf import -no-color tart_vm.imported fixture-base
tf plan -detailed-exitcode -no-color
expect_failure 'Tart deletion blocked' tf plan -destroy -no-color
test -f "$TART_HOME/vms/fixture-base/disk.img"
tf state list | grep -Fxq tart_vm.imported

"$tart" rename fixture-base fixture-original
"$tart" create fixture-base --linux --disk-size 8
"$tart" set fixture-base --cpu 2 --memory 2048
expect_failure 'VM identity does not match Terraform state' tf plan -no-color
test -f "$TART_HOME/vms/fixture-original/disk.img"
test -f "$TART_HOME/vms/fixture-base/disk.img"
echo 'Passed Tart lookup, clone, no-change, drift, resize, running guard, import, deletion, and replacement checks.'
