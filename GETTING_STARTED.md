# gpu-run Getting Started

`gpu-run` は、あらかじめ用意したレシピを指定するだけで、GPUクラウド上の学習環境を起動し、素材の転送、学習、成果物の回収、Podの削除まで行うCLIです。

現在の実装は **RunPod Secure Cloud + sd-scriptsのSDXL LoRA** に対応しています。

## 1. 前提ツール

- macOSまたはLinux
- Go 1.22以上
- `runpodctl`
- RunPod API key

`runpodctl` はRunPodの公式CLIをインストールし、ログインできる状態にしておきます。

```sh
runpodctl doctor
```

## 2. CLIをビルドする

```sh
git clone https://github.com/dyamagishi/gpuaas-runner.git
cd gpuaas-runner
go build -o gpu-run ./cmd/gpu-run
```

## 3. API keyを設定する

リポジトリ直下の `.env` にRunPod API keyを書きます。

```dotenv
RUNPOD_API_KEY=xxxxxxxxxxxxxxxx
```

`.env` はCLI起動時に自動で読み込まれます。Gitへコミットしないでください。

## 4. 実行上限を設定する

初回だけ実行します。

```sh
./gpu-run config init
```

設定ファイルは `~/.config/gpu-run/config.yaml` に作成されます。GPU、最大実行時間、最大ディスク容量、最大時間単価をここで制限できます。

例えばA40だけを許可する場合は、次のようにします。

```yaml
max_hourly_usd: 1
max_runtime: 1800s
max_disk_gb: 100
allowed_gpu_ids:
  - NVIDIA A40
```

レシピの要求とこの設定の両方を満たす場合だけPodが作成されます。

## 5. データセットを準備する

SDXL LoRAレシピでは、画像と同名のcaption `.txt` を配置します。

```text
KAKUCHI_NENE/
└── image/
    └── 50_nene/
        ├── 001.png
        ├── 001.txt
        ├── 002.png
        └── 002.txt
```

`dataset_dir` には `image` ディレクトリを含む親ディレクトリを指定します。

## 6. レシピを検証する

レシピは、実行環境・必要GPU・入力項目・学習コマンド・成果物を定義したYAMLです。

```sh
./gpu-run recipe validate recipes/sdxl-lora.yaml \
  --input dataset_dir=/Volumes/ssd01/data/LoRA/KAKUCHI_NENE \
  --input base_model=https://huggingface.co/cagliostrolab/animagine-xl-4.0/resolve/main/animagine-xl-4.0.safetensors \
  --input output_name=kakuchi_nene \
  --input max_train_steps=10
```

検証では、入力不足、存在しないパス、不正なイメージ指定、未解決の変数などを実行前に検出します。

## 7. 学習を実行する

```sh
./gpu-run run recipes/sdxl-lora.yaml \
  --input dataset_dir=/Volumes/ssd01/data/LoRA/KAKUCHI_NENE \
  --input base_model=https://huggingface.co/cagliostrolab/animagine-xl-4.0/resolve/main/animagine-xl-4.0.safetensors \
  --input output_name=kakuchi_nene \
  --input max_train_steps=10
```

実行中は、次の処理が自動で行われます。

1. RunPodで条件に合うGPU Podを起動
2. データセットをPodへ転送
3. `base_model` のHTTPSファイルをPod内で取得
4. レシピの学習コマンドを実行
5. ログとsafetensorsをローカルへ回収
6. Podを削除して課金を停止

本番学習では `max_train_steps=10` を `1000` や `2000` などに変更します。

## 8. 成果物と実行履歴を確認する

成果物はデフォルトで次に保存されます。

```text
runs/<recipe-name>/<run-id>/
├── checkpoints/
├── stdout.log
├── stderr.log
└── status.json
```

実行履歴を一覧表示します。

```sh
./gpu-run runs list
```

個別の状態をJSONで確認します。

```sh
./gpu-run status <run-id>
```

## レシピを増やす

レシピを追加すると、同じCLIで別の学習処理を実行できます。

```sh
./gpu-run run recipes/musubi-tuner.yaml \
  --input dataset_dir=/path/to/data \
  --input output_name=my-model
```

レシピには主に次を定義します。

- 使用するコンテナイメージ
- GPU/VRAM/ディスク要件
- 入力値（素材、モデルURL、ハイパーパラメータ）
- リモートで実行するargv
- 回収する成果物

そのため、利用者は毎回Docker、SSH、ファイル転送、クラウドPodの削除を手作業で行う必要がありません。

## 現在の制限

- 対応プロバイダはRunPodのみ
- `musubi-tuner` 用レシピはまだ未同梱
- `--detach`、attach/recoverによる途中再接続は未実装
- remote secretの注入は未実装
- 成果物manifestのSHA-256をCLI側で自動照合する機能は未実装

詳細なスキーマと運用上の注意点は、[`docs/recipe-schema.md`](docs/recipe-schema.md) と [`docs/operations.md`](docs/operations.md) を参照してください。
