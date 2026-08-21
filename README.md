# gpu-run

Recipeを呼び出してGPUクラウド上の学習を実行するCLI（v1はRunpod Secure Cloud + sd-scripts SDXL LoRA）。

初めて使う場合は [`gettingstart.md`](gettingstart.md) を参照してください。

```sh
gpu-run recipe validate recipes/sdxl-lora.yaml --input dataset_dir=./dataset --input base_model=./sdxl-base --input output_name=my-lora
gpu-run run recipes/sdxl-lora.yaml --input dataset_dir=./dataset --input base_model=./sdxl-base --input output_name=my-lora
```

`recipes/sdxl-lora.yaml` のimageは公開GHCR digestへ置換してから利用する。タグやプレースホルダーはCLIが拒否する。base modelは公開HTTPSの`.safetensors` checkpointをPod内へ直接取得する。入力名・成果物名・hard limit precedenceは [`docs/recipe-schema.md`](docs/recipe-schema.md) に固定している。secretと課金停止を含む運用は [`docs/operations.md`](docs/operations.md) を参照。

## Status

Runpodctl経由のPod作成、SSH/rsync転送、remote runner起動、artifact回収、cleanupまでのCLI経路とMock/unit testsを実装済みです。2026-07-26にRunPod実機でAnimagine XL 4.0をベースに10 stepのSDXL LoRA学習を完走し、safetensors 2本を回収、Pod削除まで確認しました。Vast.ai、musubi-tuner、attach/recover再接続、remoteへのsecret注入、artifact manifestのCLI側SHA照合は未実施/未実装です。`Dockerfile` はsd-scripts commit `37a1cbbc5725ed2a3575506e7bd2001c9908ac92` とCUDA/PyTorch依存を固定する。Docker buildには`CUDA_BASE_IMAGE` repository variable（`@sha256:` 64桁digest）が必須で、placeholderのままでは失敗する。
