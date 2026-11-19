#!/usr/bin/env bash
set -euo pipefail

version=${1:?specify a version}
build=${2:?specify a build number}
[[ $version =~ ^[0-9]+\.[0-9]+\.[0-9]+$ && $build =~ ^[0-9]+$ ]] || { echo 'Invalid package version' >&2; exit 2; }
root=$(cd -- "$(dirname -- "$0")/../.." && pwd)
output="$root/dist/portable"
mkdir -p "$output"
cd "$root/course-worker/cmd/buct-portable"
if [[ -e AndroidManifest.xml ]]; then echo 'Move the custom AndroidManifest.xml before packaging.' >&2; exit 2; fi
stage=$(mktemp -d)
chmod 700 "$stage"
trap 'rm -f -- AndroidManifest.xml; rm -rf -- "$stage"' EXIT

app_id=io.github.ling0727ai.buctcoursecompanion
label=android-arm64
if [[ -z ${BUCT_ANDROID_KEYSTORE_B64:-} ]]; then
    app_id+=.preview
    label+=-preview
    keystore="${BUCT_ANDROID_PREVIEW_KEYSTORE:-${XDG_CACHE_HOME:-$HOME/.cache}/buct-preview/debug.keystore}"
    mkdir -p "$(dirname -- "$keystore")"
    if [[ ! -f $keystore ]]; then
        keytool -genkeypair -keystore "$keystore" -storepass android -keypass android -alias buctpreview \
          -dname 'CN=BUCT Course Companion Preview' -keyalg RSA -keysize 2048 -validity 10000 >/dev/null
    fi
    export BUCT_ANDROID_STORE_PASSWORD=android BUCT_ANDROID_KEY_PASSWORD=android BUCT_ANDROID_KEY_ALIAS=buctpreview
else
    : "${BUCT_ANDROID_STORE_PASSWORD:?}" "${BUCT_ANDROID_KEY_PASSWORD:?}" "${BUCT_ANDROID_KEY_ALIAS:?}"
    keystore="$stage/distribution.keystore"
    printf '%s' "$BUCT_ANDROID_KEYSTORE_B64" | base64 -d > "$keystore"
    unset BUCT_ANDROID_KEYSTORE_B64
fi

binary="BUCTCourseCompanion.aab"
sed -e "s/@APP_ID@/$app_id/g" -e "s/@BUILD@/$build/g" -e "s/@VERSION@/$version/g" \
    "$root/packaging/portable/AndroidManifest.xml.in" > AndroidManifest.xml
printf '%s\n' "$BUCT_ANDROID_STORE_PASSWORD" > "$stage/store-password"
printf '%s\n' "$BUCT_ANDROID_KEY_PASSWORD" > "$stage/key-password"
unset BUCT_ANDROID_STORE_PASSWORD BUCT_ANDROID_KEY_PASSWORD

# Fyne's distribution command creates the AAB. The intermediate key is not
# used to sign the downloadable APK; bundletool applies the selected key below.
keytool -genkeypair -keystore "$stage/intermediate.keystore" -storepass android -keypass android -alias intermediate \
    -dname 'CN=BUCT AAB Builder' -keyalg RSA -keysize 2048 -validity 7 >/dev/null
fyne release --target android/arm64 --app-id "$app_id" --name BUCTCourseCompanion --icon assets/Icon.png \
    --app-version "$version" --app-build "$build" --keystore "$stage/intermediate.keystore" \
    --keystore-pass android --key-name intermediate --key-pass android

sdk=${ANDROID_HOME:-${ANDROID_SDK_ROOT:?Android SDK is required}}
tools="$sdk/build-tools/36.0.0"
installer="$output/BUCT-Course-Companion-$version-$label.apk"
bundletool build-apks --mode=universal --bundle="$binary" --output="$stage/application.apks" \
    --ks="$keystore" --ks-key-alias="$BUCT_ANDROID_KEY_ALIAS" \
    --ks-pass="file:$stage/store-password" --key-pass="file:$stage/key-password"
unzip -p "$stage/application.apks" universal.apk > "$installer"
"$tools/apksigner" verify "$installer"
"$tools/zipalign" -c -P 16 4 "$installer"
badging=$("$tools/aapt" dump badging "$installer")
printf '%s\n' "$badging" | grep -E '^package:|^native-code:|^targetSdkVersion:'
printf '%s\n' "$badging" | grep -F "targetSdkVersion:'36'" >/dev/null
"$tools/aapt" dump permissions "$installer" | grep -F 'android.permission.POST_NOTIFICATIONS' >/dev/null
