#!/usr/bin/env bash
# Pin the build toolchain; a mirror download must also pass the SHA-256 published by the Go project.
set -euo pipefail

version=1.26.1
# Source: https://go.dev/dl/?mode=json&include=all
checksum=031f088e5d955bab8657ede27ad4e3bc5b7c1ba281f05f245bcc304f327c987a
install_dir="/opt/go-${version}"

if [ ! -x "$install_dir/bin/go" ]; then
  tmp="$(mktemp -d)"
  trap 'rm -rf "$tmp"' EXIT
  filename="go${version}.linux-amd64.tar.gz"
  downloaded=false
  for base in https://golang.google.cn/dl https://mirrors.aliyun.com/golang https://go.dev/dl; do
    if curl -fsSL --connect-timeout 15 --max-time 240 "$base/$filename" -o "$tmp/$filename"; then
      printf '%s  %s\n' "$checksum" "$filename" > "$tmp/SHA256SUMS"
      if (cd "$tmp" && sha256sum -c SHA256SUMS); then
        downloaded=true
        break
      fi
    fi
  done
  test "$downloaded" = true || { echo 'could not obtain a verified Go toolchain' >&2; exit 1; }
  tar -C "$tmp" -xzf "$tmp/$filename"
  mv "$tmp/go" "$install_dir"
fi

test "$("$install_dir/bin/go" env GOVERSION)" = "go$version"
"$install_dir/bin/go" version
printf '%s/bin\n' "$install_dir" >> "$GITHUB_PATH"
