#!/usr/bin/env bash
set -euo pipefail

platform=${1:?specify linux or macos}
version=${2:?specify a version}
[[ $version =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]] || { echo 'Use a numeric version such as 0.1.0' >&2; exit 2; }
root=$(cd -- "$(dirname -- "$0")/../.." && pwd)
output="$root/dist/portable"
stage=$(mktemp -d)
trap 'rm -rf -- "$stage"' EXIT
mkdir -p "$output"
icon="$root/course-worker/cmd/buct-portable/assets/Icon.png"

cd "$root/course-worker"
go build -trimpath -ldflags='-s -w' -o "$stage/buct-course-companion" ./cmd/buct-portable

case "$platform" in
  linux)
    arch=$(dpkg --print-architecture)
    package="$stage/deb"
    mkdir -p "$package"/{DEBIAN,usr/bin,usr/share/applications,usr/share/icons/hicolor/512x512/apps,usr/share/doc/buct-course-companion}
    install -m 0755 "$stage/buct-course-companion" "$package/usr/bin/buct-course-companion"
    install -m 0644 "$icon" "$package/usr/share/icons/hicolor/512x512/apps/buct-course-companion.png"
    install -m 0644 "$root/LICENSE" "$package/usr/share/doc/buct-course-companion/copyright"
    install -m 0644 "$root/course-worker/cmd/buct-portable/assets/OFL.txt" "$package/usr/share/doc/buct-course-companion/NotoSansSC-OFL.txt"
    cat > "$package/DEBIAN/control" <<EOF
Package: buct-course-companion
Version: $version
Section: education
Priority: optional
Architecture: $arch
Maintainer: LingXin <173931019+Ling0727-ai@users.noreply.github.com>
Depends: libc6, libgl1, libx11-6, libxcursor1, libxi6, libxinerama1, libxrandr2, libxxf86vm1
Description: BUCT Course Companion
 Desktop companion for assignments, reminders and timetables at BUCT.
EOF
    cat > "$package/usr/share/applications/io.github.ling0727ai.buctcoursecompanion.desktop" <<'EOF'
[Desktop Entry]
Type=Application
Name=BUCT Course Companion
Name[zh_CN]=北化课伴
Exec=/usr/bin/buct-course-companion
Icon=buct-course-companion
Categories=Education;Utility;
Terminal=false
EOF
    dpkg-deb --root-owner-group --build "$package" "$output/BUCT-Course-Companion-$version-linux-$arch.deb"
    ;;
  macos)
    arch=$(uname -m)
    bundle="$stage/BUCT Course Companion.app"
    mkdir -p "$bundle/Contents/MacOS" "$bundle/Contents/Resources" "$stage/Application.iconset"
    install -m 0755 "$stage/buct-course-companion" "$bundle/Contents/MacOS/buct-course-companion"
    install -m 0644 "$root/course-worker/cmd/buct-portable/assets/OFL.txt" "$bundle/Contents/Resources/NotoSansSC-OFL.txt"
    for size in 16 32 128 256 512; do
      sips -z "$size" "$size" "$icon" --out "$stage/Application.iconset/icon_${size}x${size}.png" >/dev/null
      sips -z "$((size*2))" "$((size*2))" "$icon" --out "$stage/Application.iconset/icon_${size}x${size}@2x.png" >/dev/null
    done
    iconutil -c icns "$stage/Application.iconset" -o "$bundle/Contents/Resources/Application.icns"
    cat > "$bundle/Contents/Info.plist" <<EOF
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0"><dict>
<key>CFBundleIdentifier</key><string>io.github.ling0727ai.buctcoursecompanion</string>
<key>CFBundleName</key><string>BUCT Course Companion</string>
<key>CFBundleDisplayName</key><string>北化课伴</string>
<key>CFBundlePackageType</key><string>APPL</string>
<key>CFBundleExecutable</key><string>buct-course-companion</string>
<key>CFBundleIconFile</key><string>Application.icns</string>
<key>CFBundleShortVersionString</key><string>$version</string>
<key>CFBundleVersion</key><string>$version</string>
<key>LSApplicationCategoryType</key><string>public.app-category.education</string>
<key>NSHighResolutionCapable</key><true/>
</dict></plist>
EOF
    plutil -lint "$bundle/Contents/Info.plist"
    codesign --force --sign - "$bundle"
    pkgbuild --component "$bundle" --install-location /Applications "$output/BUCT-Course-Companion-$version-macos-$arch.pkg"
    ;;
  *)
    echo "Unsupported platform: $platform" >&2
    exit 2
    ;;
esac
