#!/bin/sh
set -eu

template_path=${1:-./mihomo/config.yaml.tmpl}
output_path=${2:-/data/sub2api-v2/proxy-pool/mihomo/config.yaml}

: "${MIHOMO_SECRET:?MIHOMO_SECRET is required}"

mkdir -p "$(dirname "$output_path")"
python3 - "$template_path" "$output_path" <<'PY'
import os
import pathlib
import sys

template = pathlib.Path(sys.argv[1]).read_text(encoding="utf-8")
rendered = template.replace("__MIHOMO_SECRET__", os.environ["MIHOMO_SECRET"])
output = pathlib.Path(sys.argv[2])
temporary = output.with_suffix(output.suffix + ".tmp")
temporary.write_text(rendered, encoding="utf-8")
temporary.chmod(0o600)
temporary.replace(output)
PY
