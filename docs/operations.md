# 運用とsecret

必要なsecretはローカル環境変数 `RUNPOD_API_KEY`（Pod API）のみ。v1のsample recipeは公開HTTPSのSDXL `.safetensors` checkpointをPod内で直接取得するため、HF tokenをremoteへ注入しない。recipeで `secret: true` の入力やenv注入を指定した場合は、Pod作成前に拒否する。将来secret入力を実装する場合も、ファイル、recipe、SQLite、イベントログへ値を書かない。CIではGitHub Actions secretへ登録し、workflowのログマスキングを有効にする。

実行前に `gpu-run config init` で `max_hourly_usd`、`max_runtime`、`max_disk_gb`、`allowed_gpu_ids` を必ず設定する。Pod作成時はRunpod側の `terminateAfter` に絶対期限を設定し、価格が上限を超えたら転送前に破棄する。foregroundのCtrl-Cはcleanupを試みて終了する。detach/attachはv1では未実装である。

成果物は一時pathへ取得してatomic renameする。現行v1はremote manifestのSHA-256照合をまだ行わないため、checksum保証が必要な運用ではE2E検証完了まで使用しない。cleanupが失敗した場合はrunを `cleanup_required` として記録する。保存されたPod IDを確認して手動削除する。attach/recoverは未実装で、SIGINTはcleanupを試みてforeground runを終了する。

## Runpod E2E（手動承認）

1. `export RUNPOD_API_KEY=...`（sample recipeはローカルbase-modelを使うためHF tokenは不要）。
2. recipeのimageを公開済みdigestへ置換し、`gpu-run recipe validate recipes/sdxl-lora.yaml --input ...` を実行。
3. 少量データ・少stepで `gpu-run run ...` を実行し、ログ追従、Ctrl-C時のcleanup、Pod消滅を確認。checksum検証は未実装のため、別途手動で確認する。
4. 失敗ケースではlog回収とPod destroyを確認する。E2E後、managed Podが0件であることをRunpod console/APIで確認する。

CIの有料E2Eは `workflow_dispatch` のみで、forkやpushでは実行しない。

## 実測済みSDXL smoke

2026-07-26、A40（$0.44/h）でAnimagine XL 4.0 checkpointをPod内HTTPS取得し、KAKUCHI_NENEのPNG/TXT 10組を10 step学習した。平均lossは約0.0455。`runs/sdxl-lora/af55d47133af4dd4/checkpoints/` に `kakuchi_nene.safetensors` とstep checkpointを回収し、Podが0件になったことを確認した。

## Remote runner status

remote runnerは `/opt/gpu-run/remote-runner start RUN_DIR ARGV_FILE WORKING_DIR [ARTIFACT_ROOT]` で起動し、内部のrun modeが実処理を行う。`ARGV_FILE` はNUL区切りのargvで、shell文字列や`eval`は使わない。runnerは `status.json`、`stdout.log`、`stderr.log`、`artifact-manifest.jsonl`（JSONエンコード済みpath/size/SHA-256）と終了コードをrun directoryへ書く。manifestをatomic renameしてからterminal statusを書き込むが、CLI側のSHA-256照合は未実装である。artifactの `when` とstageの `timeout` もv1では実行制御に未適用である。image entrypointはsshdをforegroundで保持し、root password loginは無効。CLIのSSH鍵配布・attach/recover統合とRunpod E2Eは未実施。
