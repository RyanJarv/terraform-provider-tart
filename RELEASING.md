# Prepare a Registry release

Build on Apple silicon with the project's approved Go toolchain and dependency
cache. The provider supports `darwin_arm64`, uses plugin protocol 6, and serves
`registry.terraform.io/ryanjarv/tart`.

Use a clean checkout at the intended `vMAJOR.MINOR.PATCH` tag. Choose a new
version when changing the contents of a release. The following builds the
archive, manifest and checksum file into a new temporary release directory.

```sh
set -eu
version="$(git describe --tags --exact-match HEAD)"
version="${version#v}"
release="$(mktemp -d)"
binary="terraform-provider-tart_v$version"
prefix="terraform-provider-tart_$version"
GOTOOLCHAIN=local GOWORK=off GOPROXY=off GOSUMDB=off go -C provider test -mod=readonly ./...
GOTOOLCHAIN=local GOWORK=off GOPROXY=off GOSUMDB=off go -C tart test -mod=readonly ./...
GOTOOLCHAIN=local GOWORK=off GOPROXY=off GOSUMDB=off CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 \
  go -C provider build -mod=readonly -trimpath -ldflags "-s -w -X main.version=$version" -o "$release/$binary" .
cp terraform-registry-manifest.json "$release/${prefix}_manifest.json"
cp LICENSE "$release/"
(
  cd "$release"
  zip "${prefix}_darwin_arm64.zip" "$binary" LICENSE
  shasum -a 256 "${prefix}_darwin_arm64.zip" "${prefix}_manifest.json" > "${prefix}_SHA256SUMS"
)
printf '%s\n' "$release"
```

Before publication, run the VM lifecycle test on a disposable store. Sign the
checksum file with the Registry's enrolled GPG release key using
`gpg --detach-sign --output "$release/${prefix}_SHA256SUMS.sig" "$release/${prefix}_SHA256SUMS"`.
Verify the signature and checksums. Attach the ZIP, manifest, checksum file,
and binary signature to a GitHub release at the matching tag. The GitHub
repository must be public before registering it with the Terraform Registry.
Review the repository history and commit metadata before making it public.
[HashiCorp publishing requirements](https://developer.hashicorp.com/terraform/registry/providers/publishing)

Existing local builds may use a different provider address in state. Review a
state backup and Terraform's `state replace-provider` procedure before
switching existing deployments to `ryanjarv/tart`.
