#!/usr/bin/env bash
set -euo pipefail

version=${1:?specify a version}
cd -- "${2:?specify the directory with installers}"
prefix="BUCT-Course-Companion-$version"
android="$prefix-android-arm64.apk"
if [[ ! -f $android ]]; then
  android="$prefix-android-arm64-preview.apk"
fi
installers=(
  "$prefix-Setup-x64.exe"
  "$prefix-linux-amd64.deb"
  "$prefix-macos-arm64.pkg"
  "$prefix-macos-x86_64.pkg"
  "$android"
)

for installer in "${installers[@]}"; do
  if [[ ! -s $installer ]]; then
    echo "Missing or empty installer: $installer" >&2
    exit 1
  fi
done
shopt -s nullglob
found=(*.exe *.deb *.pkg *.apk)
if ((${#found[@]} != ${#installers[@]})); then
  echo "Expected exactly five installers; found: ${found[*]}" >&2
  exit 1
fi

sha256sum -- "${installers[@]}" > SHA256SUMS.txt
cat SHA256SUMS.txt
if [[ -n ${GITHUB_STEP_SUMMARY:-} ]]; then
  {
    printf '### 安装包 %s\n\n' "$version"
    printf -- '- `%s`\n' "${installers[@]}"
    printf '\n校验文件位于 `release-checksums` 构建产物。\n'
  } >> "$GITHUB_STEP_SUMMARY"
fi
