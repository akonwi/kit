#!/bin/sh
# Archives the Release app (com.akonwi.kit.ios) and uploads it to App Store
# Connect for TestFlight, authenticated with an App Store Connect API key:
#
#   ASC_KEY_ID=… ASC_ISSUER_ID=… ASC_KEY_PATH=~/Downloads/AuthKey_….p8 \
#     apps/ios/script/upload_testflight.sh [build-number]
#
# The build number defaults to the UTC date and time, YYYYMMDD.HHMM, so each
# upload is higher than the last. The version is MARKETING_VERSION in the
# project. Uploads only a committed tree, and prints the commit it built.
#
# The export signs with the team's iOS distribution certificate, which must be
# in the login keychain, and the "Kit iOS App Store" provisioning profile (see
# ExportOptions.plist): an App Manager key can't use Apple's cloud signing.
set -eu

: "${ASC_KEY_ID:?Set ASC_KEY_ID to the App Store Connect API key ID}"
: "${ASC_ISSUER_ID:?Set ASC_ISSUER_ID to the key's issuer ID}"
: "${ASC_KEY_PATH:?Set ASC_KEY_PATH to the key's .p8 file}"
[ -r "$ASC_KEY_PATH" ] || { echo "Can't read $ASC_KEY_PATH." >&2; exit 1; }

script_dir=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
app_dir=$(CDPATH= cd -- "$script_dir/.." && pwd)
cd "$app_dir"
if [ -n "$(git status --porcelain -- . ../apple)" ]; then
  echo "Commit the iOS and shared Apple changes before uploading." >&2
  exit 1
fi
build=${1:-$(date -u +%Y%m%d.%H%M)}
commit=$(git rev-parse --short HEAD)
archive=".build/testflight/Kit-$build.xcarchive"
auth="-allowProvisioningUpdates -authenticationKeyPath $ASC_KEY_PATH -authenticationKeyID $ASC_KEY_ID -authenticationKeyIssuerID $ASC_ISSUER_ID"

echo "Archiving build $build from $commit"
# shellcheck disable=SC2086
xcodebuild -project Kit.xcodeproj -scheme Kit -configuration Release \
  -destination 'generic/platform=iOS' -derivedDataPath .build/testflight/derived \
  -skipPackagePluginValidation -archivePath "$archive" \
  CURRENT_PROJECT_VERSION="$build" $auth archive

echo "Uploading build $build"
# shellcheck disable=SC2086
xcodebuild -exportArchive -archivePath "$archive" \
  -exportOptionsPlist "$script_dir/ExportOptions.plist" \
  -exportPath ".build/testflight/export-$build" $auth

echo "Uploaded build $build from $commit. It appears in TestFlight once App Store Connect processes it."
