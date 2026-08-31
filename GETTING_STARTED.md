# gpu-run Getting Started

`gpu-run` は、GPUを必要とする処理を「レシピ」として定義し、同じCLIからクラウド上で実行するためのジョブランナーです。

利用者が意識するのは、基本的に次の3つだけです。

```text
レシピ       = どの環境で、何を実行するか
入力         = 今回使うデータ、モデル、パラメータ
成果物       = 実行後に手元へ戻すファイル
```

GPU Podの起動、ファイル転送、リモート実行、ログ取得、成果物回収、環境削除はランナーが担当します。

## 1. まず動かす

### 必要なもの

- macOSまたはLinux
- Go 1.22以上
- `runpodctl`
- GPUプロバイダのAPI key

現在のv1では、プロバイダとしてRunPod Secure Cloudを使用します。

### CLIをビルドする

```sh
git clone https://github.com/dyamagishi/gpuaas-runner.git
cd gpuaas-runner
go build -o gpu-run ./cmd/gpu-run
```

### API keyを設定する

リポジトリ直下の `.env` に設定します。

```dotenv
RUNPOD_API_KEY=xxxxxxxxxxxxxxxx
```

`.env` はCLI起動時に自動読込されます。Gitへコミットしないでください。

### 実行上限を設定する

```sh
./gpu-run config init
```

`~/.config/gpu-run/config.yaml` で、許可GPU、最大実行時間、最大ディスク容量、最大時間単価を設定します。これらは安全側のハードリミットで、レシピやCLI引数で緩和できません。

## 2. レシピを理解する

レシピは、特定の学習ツールに限らないジョブ定義です。主に次を持ちます。

- 実行するコンテナイメージ（digest固定）
- 必要GPU、VRAM、ディスク、CUDA条件
- ユーザーから受け取る入力値
- リモートで実行するargv
- ローカルから転送する入力
- 回収する成果物

例えば、レシピは次のような責務を持ちます。

```yaml
name: some-gpu-job
image: ghcr.io/example/job@sha256:<64-hex-digest>

inputs:
  dataset_dir: {type: directory, required: true}
  output_name: {type: string, required: true}

stages:
  - id: main
    working_dir: /opt/app
    argv:
      - /opt/app/run-job
      - --dataset=${transfers.dataset_dir}
      - --output=/workspace/output
      - --name=${inputs.output_name}

artifacts:
  - name: output
    kind: directory
    remote_path: output
    required: true
```

レシピを追加すれば、同じランナーで別の学習、変換、評価、生成ジョブを扱えます。ツール固有の引数はレシピ側に閉じ込め、CLIの使い方は共通に保ちます。

## 3. 入力を渡して実行する

まず、入力値とレシピが整合するか検証します。

```sh
./gpu-run recipe validate recipes/<recipe>.yaml \
  --input dataset_dir=/path/to/dataset \
  --input output_name=my-result
```

問題がなければ実行します。

```sh
./gpu-run run recipes/<recipe>.yaml \
  --input dataset_dir=/path/to/dataset \
  --input output_name=my-result
```

ランナーは次の順で処理します。

1. レシピと入力を検証
2. hard limit内でGPU Podを起動
3. 必要な入力ファイルをPodへ転送
4. レシピのstageをリモート実行
5. ログと成果物をローカルへ回収
6. Podを削除して課金を停止

## 4. 結果と状態を確認する

成果物はデフォルトで次に保存されます。

```text
runs/<recipe-name>/<run-id>/
├── <artifact directories or files>
├── stdout.log
├── stderr.log
└── status.json
```

実行履歴を一覧表示します。

```sh
./gpu-run runs list
```

個別の状態を確認します。

```sh
./gpu-run status <run-id>
```

失敗時はrunディレクトリのログとdiagnosticsを確認します。途中で停止したPodを明示的に削除する場合は、状態を確認してから次を実行します。

```sh
./gpu-run cancel <run-id>
./gpu-run cleanup <run-id>
```

## 5. 動作確認済みの例

現在同梱されている実レシピは、sd-scriptsによるSDXL LoRAと Anima LoRAです。どちらもプロダクトの目的そのものではなく、レシピ駆動の実行経路を確認するサンプルです。SDXLは [`examples/sdxl-sd-scripts.md`](examples/sdxl-sd-scripts.md)、Anima（専用イメージ `Dockerfile.anima`）は [`examples/anima-sd-scripts.md`](examples/anima-sd-scripts.md) に分離しています。

```sh
./gpu-run run recipes/sdxl-lora.yaml \
  --input dataset_dir=/path/to/dataset \
  --input base_model=https://example.com/base-model.safetensors \
  --input output_name=my-lora \
  --input max_train_steps=10
```

このレシピでは、ベースモデルはPod内でHTTPS取得し、データセットだけをローカルから転送します。本番学習では `max_train_steps` などを用途に合わせて変更します。

## 現在の範囲と今後の拡張

現在のv1で実装済みなのは、RunPod Secure Cloud、digest固定コンテナ、レシピ入力解決、SSH/rsync転送、リモートrunner、成果物回収、cleanupです。

今後は同じレシピインターフェースに対して、次を追加していく想定です。

- Vast.aiプロバイダ
- musubi-tunerなどのレシピ
- attach/recoverによる再接続
- checkpointからの再開
- remote secretの安全な注入
- 成果物manifestのCLI側ハッシュ検証

レシピの詳細仕様は [`docs/recipe-schema.md`](docs/recipe-schema.md)、運用上の制約は [`docs/operations.md`](docs/operations.md) を参照してください。
