#!/usr/bin/env bash
set -euo pipefail

# Execute a pre-built NUL-delimited argv file without eval or shell expansion.
# Usage: remote-runner.sh run RUN_DIR ARGV_FILE WORKING_DIR [ARTIFACT_ROOT]
#        remote-runner.sh start RUN_DIR ARGV_FILE WORKING_DIR [ARTIFACT_ROOT]
usage() {
  echo "usage: $0 run RUN_DIR ARGV_FILE WORKING_DIR [ARTIFACT_ROOT]" >&2
  exit 2
}

if [[ "${1:-}" == start ]]; then
  shift
  [[ $# -ge 4 && $# -le 5 ]] || usage
  run_dir=$1
  mkdir -p -- "$run_dir"
  nohup "$0" run "$@" >"$run_dir/runner-start.log" 2>&1 < /dev/null &
  echo "$!"
  exit 0
fi
[[ "${1:-}" == run ]] || usage
[[ $# -ge 4 && $# -le 5 ]] || usage
run_dir=$2
argv_file=$3
working_dir=$4
artifact_root=${5:-"$run_dir"}

[[ -f "$argv_file" ]] || { echo "argv file not found: $argv_file" >&2; exit 2; }
mkdir -p -- "$run_dir"
run_real=$(realpath -m -- "$run_dir")
artifact_real=$(realpath -m -- "$artifact_root")
case "$artifact_real" in
  "$run_real"|"$run_real"/*) ;;
  *) echo "artifact root must be inside run directory" >&2; exit 2 ;;
esac
stdout_log="$run_dir/stdout.log"
stderr_log="$run_dir/stderr.log"
status_file="$run_dir/status.json"
manifest_file="$run_dir/artifact-manifest.jsonl"
manifest_tmp="$run_dir/.artifact-manifest.jsonl.tmp.$$"

timestamp() { date -u +%Y-%m-%dT%H:%M:%SZ; }
write_status() {
  local phase=$1 code=$2
  printf '{"phase":"%s","exit_code":%s,"updated_at":"%s"}\n' \
    "$phase" "$code" "$(timestamp)" >"$status_file"
}

argv=()
while IFS= read -r -d '' arg; do
  argv+=("$arg")
done <"$argv_file" || true
(( ${#argv[@]} > 0 )) || { echo "argv file is empty" >&2; write_status failed 2; exit 2; }

write_status running null
set +e
( cd -- "$working_dir" && "${argv[@]}" ) >"$stdout_log" 2>"$stderr_log"
exit_code=$?
set -e

if (( exit_code == 0 )); then
  phase=completed
else
  phase=failed
fi
# Encode paths with Python's JSON encoder, then atomically publish the manifest
# before terminal status is written.
python3 - "$artifact_real" "$manifest_tmp" "$run_real" <<'PY'
import hashlib, json, pathlib, sys
root, output, run_dir = map(pathlib.Path, sys.argv[1:])
with output.open("w", encoding="utf-8") as out:
    if root.is_dir():
        for path in sorted(root.rglob("*")):
            if not path.is_file() or path.is_symlink():
                continue
            if path in {run_dir / "status.json", run_dir / "artifact-manifest.jsonl"} \
                    or path.name.startswith(".artifact-manifest.jsonl.tmp."):
                continue
            digest = hashlib.sha256()
            size = 0
            with path.open("rb") as src:
                for chunk in iter(lambda: src.read(1024 * 1024), b""):
                    size += len(chunk)
                    digest.update(chunk)
            out.write(json.dumps({"path": str(path.relative_to(root)), "size": size,
                                  "sha256": digest.hexdigest()}, ensure_ascii=True) + "\n")
PY
mv -f -- "$manifest_tmp" "$manifest_file"
write_status "$phase" "$exit_code"
exit "$exit_code"
