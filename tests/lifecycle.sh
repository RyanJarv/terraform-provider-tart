#!/bin/sh
# Run on Apple silicon with an already built provider and available Tart/Terraform binaries.
set -eu

tart_override=
case "$#" in
    2) terraform=$(command -v terraform) || { echo 'terraform missing from PATH' >&2; exit 2; }
       tart=$(command -v tart) || { echo 'tart missing from PATH' >&2; exit 2; } ;;
    4) terraform=$1; tart=$2; tart_override="tart_path = \"$tart\""; shift 2 ;;
    *) echo "Usage: $0 [/abs/terraform /abs/tart] /abs/provider-bin-dir /abs/ubuntu-arm64.iso" >&2
       exit 2 ;;
esac
provider_dir=$1
installer=$2
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
  $tart_override
  tart_home = "$TART_HOME"
}
CONFIG
done

cat > "$fixture/create/main.tf" <<'CONFIG'
variable "base_name" {
  default = "fixture-base"
}
variable "clone_name" {
  default = "fixture-clone"
}
variable "cpus" {
  type    = number
  default = 2
}
variable "memory" {
  type    = number
  default = 2048
}
data "tart_vm" "base" {
  name = var.base_name
}
resource "tart_vm" "scratch" {
  name         = var.clone_name
  source_image = data.tart_vm.base.name
  cpu_count    = var.cpus
  memory_mib   = var.memory
}
output "clone_id" {
  value = tart_vm.scratch.id
}
output "base_id" {
  value = data.tart_vm.base.id
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
    if [ "$(plutil -extract CPU raw "$fixture/info.json")" != "$cpu" ] ||
        [ "$(plutil -extract Memory raw "$fixture/info.json")" != "$memory" ]; then
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
test "$(tf output -raw base_id)" = fixture-base
test "$(tf output -raw clone_id)" = fixture-clone
assert_size fixture-clone 2
tf plan -detailed-exitcode -no-color

expect_failure 'Invalid CPU count' tf plan -no-color -var=clone_name=fixture-new -var=cpus=0
assert_size fixture-clone 2
tf plan -no-color -var=clone_name=fixture-new -out=rename.tfplan
tf apply -no-color rename.tfplan
test ! -e "$TART_HOME/vms/fixture-clone"
assert_size fixture-new 2
tf apply -auto-approve -no-color
assert_size fixture-clone 2
test ! -e "$TART_HOME/vms/fixture-new"

"$tart" clone fixture-base fixture-next-base
touch "$TART_HOME/vms/fixture-clone/before-replacement"
export TF_VAR_base_name=fixture-next-base
tf plan -no-color -out=image.tfplan
tf apply -no-color image.tfplan
test ! -e "$TART_HOME/vms/fixture-clone/before-replacement"
assert_size fixture-clone 2

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
tf apply -auto-approve -no-color
assert_size fixture-clone 2 2048
test "$(plutil -extract State raw "$fixture/info.json")" = running
tf plan -detailed-exitcode -no-color
"$tart" stop fixture-clone --timeout 5
wait "$running_pid" || true
trap - EXIT

tf plan -destroy -out=destroy.tfplan -no-color
# A saved destroy targets the current VM at the approved name.
"$tart" rename fixture-clone fixture-former
"$tart" clone fixture-base fixture-clone
tf apply -no-color destroy.tfplan
assert_size fixture-former 2
test ! -e "$TART_HOME/vms/fixture-clone"
assert_size fixture-base 2

cd "$fixture/import"
tf import -no-color tart_vm.imported fixture-base
tf plan -detailed-exitcode -no-color
test -f "$TART_HOME/vms/fixture-base/disk.img"
tf state list | grep -Fxq tart_vm.imported

"$tart" rename fixture-base fixture-original
"$tart" create fixture-base --linux --disk-size 8
"$tart" set fixture-base --cpu 2 --memory 2048
tf plan -detailed-exitcode -no-color
test -f "$TART_HOME/vms/fixture-original/disk.img"
test -f "$TART_HOME/vms/fixture-base/disk.img"
cat > protected_override.tf <<'CONFIG'
resource "tart_vm" "imported" {
  lifecycle {
    prevent_destroy = true
  }
}
CONFIG
expect_failure 'lifecycle.prevent_destroy' tf plan -destroy -no-color
mv protected_override.tf protected_override.tf.disabled
tf destroy -auto-approve -no-color
test ! -e "$TART_HOME/vms/fixture-base"
test -f "$TART_HOME/vms/fixture-original/disk.img"
cd "$fixture/create"
tf apply -auto-approve -no-color -var=base_name=fixture-original
"$tart" delete fixture-clone
tf apply -auto-approve -no-color -var=base_name=fixture-original
assert_size fixture-clone 2
tf destroy -auto-approve -no-color -var=base_name=fixture-original
for vm in fixture-original fixture-former fixture-next-base; do
    "$tart" delete "$vm"
done
echo 'Passed Tart clone, drift, resize, import, replacement, name reuse, deletion, and recreation checks.'
