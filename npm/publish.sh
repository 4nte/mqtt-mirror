#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
DIST_DIR="${SCRIPT_DIR}/../dist"
VERSION="${VERSION:?VERSION environment variable must be set}"

# Map: npm-package-dir  goreleaser-os  goreleaser-arch  binary-name
PLATFORMS=(
  "linux-x64|linux|amd64|mqtt-mirror"
  "linux-arm64|linux|arm64|mqtt-mirror"
  "darwin-x64|darwin|amd64|mqtt-mirror"
  "darwin-arm64|darwin|arm64|mqtt-mirror"
  "win32-x64|windows|amd64|mqtt-mirror.exe"
)

publish_platform() {
  local entry="$1"
  IFS='|' read -r npm_dir goos goarch binary_name <<< "$entry"
  local pkg_dir="${SCRIPT_DIR}/${npm_dir}"

  # Find the binary in goreleaser dist output
  # GoReleaser creates dirs like: mqtt-mirror_linux_amd64_v1/ or mqtt-mirror_darwin_arm64/
  local bin_path=""
  for dir in "${DIST_DIR}"/mqtt-mirror_${goos}_${goarch}*/; do
    if [[ -f "${dir}${binary_name}" ]]; then
      bin_path="${dir}${binary_name}"
      break
    fi
  done

  if [[ -z "$bin_path" ]]; then
    echo "ERROR: Could not find binary for ${goos}/${goarch} in ${DIST_DIR}" >&2
    exit 1
  fi

  echo "Publishing @mqtt-mirror/${npm_dir} v${VERSION} (binary: ${bin_path})"

  cp "$bin_path" "${pkg_dir}/${binary_name}"
  chmod +x "${pkg_dir}/${binary_name}"

  # Set version in package.json
  cd "$pkg_dir"
  npm version "$VERSION" --no-git-tag-version --allow-same-version
  npm publish --access public
  cd "$SCRIPT_DIR"

  # Clean up binary
  rm -f "${pkg_dir}/${binary_name}"
}

# Publish platform packages first
for entry in "${PLATFORMS[@]}"; do
  publish_platform "$entry"
done

# Publish main package last
echo "Publishing mqtt-mirror v${VERSION}"
cd "${SCRIPT_DIR}/mqtt-mirror"

# Update version and optionalDependencies to match
npm version "$VERSION" --no-git-tag-version --allow-same-version

# Rewrite optionalDependencies versions using node
node -e "
  const fs = require('fs');
  const pkg = JSON.parse(fs.readFileSync('package.json', 'utf8'));
  for (const dep of Object.keys(pkg.optionalDependencies)) {
    pkg.optionalDependencies[dep] = '${VERSION}';
  }
  fs.writeFileSync('package.json', JSON.stringify(pkg, null, 2) + '\n');
"

npm publish --access public

echo "Done! Published mqtt-mirror@${VERSION} and all platform packages."
