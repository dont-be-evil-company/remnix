#!/usr/bin/env bash
set -euo pipefail

cp config.schema.json web/static/config.schema.json || exit 1
cp logo.png web/static/logo.png || exit 1
cp assets/images/remnix-favicon.png web/static/favicon.png || exit 1
cp assets/images/remnix-opengraph.png web/static/opengraph.png || exit 1
