#!/bin/sh
# Re-vendors TradingView Lightweight Charts into the K-line page assets at the
# version declared in internal/market/chart/assets/package.json (the file
# Dependabot updates). Needs node and npm; run from the repository root:
#
#   make update-lwc
#
# npm verifies the downloaded tarball against the registry's integrity hash.
set -eu

dir=internal/market/chart/assets
version=$(node -p "require('./$dir/package.json').dependencies['lightweight-charts']")
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT

(cd "$dir" && npm install --package-lock-only --ignore-scripts --no-audit --no-fund >/dev/null)
integrity=$(node -p "require('./$dir/package-lock.json').packages['node_modules/lightweight-charts'].integrity")
fc_version=$(node -p "require('./$dir/package-lock.json').packages['node_modules/fancy-canvas'].version")

(cd "$tmp" && npm pack "lightweight-charts@$version" --silent >/dev/null)
tar -xzf "$tmp/lightweight-charts-$version.tgz" -C "$tmp"
cp "$tmp/package/dist/lightweight-charts.standalone.production.js" "$dir/lightweight-charts.js"

{
	cat "$tmp/package/LICENSE"
	cat <<NOTICE

---------------------------------------------------------------------------
Attribution notice

TradingView Lightweight Charts™
Copyright (c) TradingView, Inc. https://www.tradingview.com/

This copy: lightweight-charts $version from the npm registry,
file dist/lightweight-charts.standalone.production.js, unmodified.
Package integrity: $integrity

The package also incorporates parts of tslib (https://github.com/Microsoft/tslib,
(c) Microsoft Corporation), covered by the BSD Zero Clause License.

---------------------------------------------------------------------------
fancy-canvas $fc_version (bundled in the standalone build)

License: MIT, as declared in its package.json (author: TradingView,
smakarov@tradingview.com). The package ships no license file; the standard
MIT terms follow.

Permission is hereby granted, free of charge, to any person obtaining a copy
of this software and associated documentation files (the "Software"), to deal
in the Software without restriction, including without limitation the rights
to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
copies of the Software, and to permit persons to whom the Software is
furnished to do so, subject to the following conditions:

The above copyright notice and this permission notice shall be included in all
copies or substantial portions of the Software.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
SOFTWARE.
NOTICE
} > "$dir/LICENSE-lightweight-charts.txt"

echo "lightweight-charts $version vendored into $dir"
