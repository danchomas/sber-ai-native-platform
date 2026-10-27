#!/usr/bin/env bash

set -euo pipefail

DIR="${DATASETS_DIR:-./datasets}"
API="${API_URL:-http://localhost:3000/api/v1}"
TIMEOUT="${UPLOAD_TIMEOUT:-1800}"

if [ ! -d "$DIR" ]; then
    echo "Каталог $DIR не найден. Положите CSV туда или задайте DATASETS_DIR." >&2
    exit 1
fi

if ! curl -sf --max-time 10 "$API/health" >/dev/null; then
    echo "Платформа не отвечает на $API — поднимите стенд: make up" >&2
    exit 1
fi

pretty_name() {
    basename "$1" .csv | sed -E 's/^[0-9]+_//; s/_/ /g'
}

shopt -s nullglob
files=()
if [ $# -eq 0 ]; then
    files=("$DIR"/*.csv)
else
    for want in "$@"; do
        for f in "$DIR"/*"$want"*.csv; do files+=("$f"); done
    done
fi

if [ ${#files[@]} -eq 0 ]; then
    echo "Подходящих файлов в $DIR не нашлось." >&2
    exit 1
fi

echo "Загружаю ${#files[@]} набор(ов) в $API"
failed=0

for f in "${files[@]}"; do
    name=$(pretty_name "$f")
    size=$(du -h "$f" | cut -f1)
    printf '  %-28s %6s  ' "$name" "$size"

    started=$(date +%s)
    if body=$(curl -sf --max-time "$TIMEOUT" -X POST "$API/datasets" \
                   -F "file=@$f" -F "name=$name" 2>&1); then
        elapsed=$(( $(date +%s) - started ))
        echo "$body" | python3 -c "
import sys, json
r = json.load(sys.stdin)
print(f\"{r['rows_loaded']} строк за ${elapsed}с\", end='')
if r.get('skipped_rows'): print(f\", пропущено {r['skipped_rows']}\", end='')
if r.get('bad_values'):   print(f\", плохих значений {r['bad_values']}\", end='')
print()
"
    else
        echo "ОШИБКА: $body"
        failed=$((failed + 1))
    fi
done

if [ "$failed" -gt 0 ]; then
    echo "Не загрузилось наборов: $failed" >&2
    exit 1
fi
echo "Готово. Каталог: $API/datasets"
