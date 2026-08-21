# Example: sd-scriptsでSDXL LoRAを学習する

これは `recipes/sdxl-lora.yaml` を使い、sd-scriptsのSDXL LoRA学習をRunPod上で実行する具体例です。`gpu-run`本体はsd-scripts専用ではなく、これはレシピの動作確認用サンプルです。

## データセット

```text
dataset/
└── image/
    └── 50_nene/
        ├── 001.png
        ├── 001.txt
        ├── 002.png
        └── 002.txt
```

画像とcaption `.txt` は同名にします。`50_nene` の `50` は、sd-scriptsでの画像repeat数です。`dataset_dir`には `image` を含む親ディレクトリを指定します。

## レシピが行う学習

Pod内でsd-scriptsの `sdxl_train_network.py` を実行します。主な設定は解像度1024、bucket有効、batch size 1、fp16、LoRA (`networks.lora`) です。データセットはPodの `/inputs/dataset` に転送され、ベースモデルは指定した公開HTTPS URLからPod内で取得されます。

## smoke test

リポジトリ直下で、まず10 stepの動作確認を行います。

```sh
./gpu-run recipe validate recipes/sdxl-lora.yaml --input dataset_dir=/path/to/dataset --input base_model=https://example.com/sdxl-base.safetensors --input output_name=my-lora --input max_train_steps=10
./gpu-run run recipes/sdxl-lora.yaml --input dataset_dir=/path/to/dataset --input base_model=https://example.com/sdxl-base.safetensors --input output_name=my-lora --input max_train_steps=10
```

10 stepはPod起動、転送、モデル取得、学習、成果物回収までを確認するための値です。本番では `max_train_steps` を用途に応じて増やします。

## Animagine XL 4.0で実行する例

```sh
ANIMAGINE_XL='https://huggingface.co/cagliostrolab/animagine-xl-4.0/resolve/main/animagine-xl-4.0.safetensors'
./gpu-run run recipes/sdxl-lora.yaml --input dataset_dir=/Volumes/ssd01/data/LoRA/KAKUCHI_NENE --input base_model="$ANIMAGINE_XL" --input output_name=kakuchi_nene --input max_train_steps=1000
```

成果物は次に保存されます。

```text
runs/sdxl-lora/<run-id>/checkpoints/kakuchi_nene.safetensors
```

## よく変更する入力

| 入力 | 内容 |
| --- | --- |
| `dataset_dir` | `image`ディレクトリを含むローカルパス |
| `base_model` | Pod内で取得する公開HTTPS checkpoint |
| `output_name` | 出力safetensorsの名前 |
| `resolution` | 学習解像度。既定値1024 |
| `train_batch_size` | バッチサイズ。既定値1 |
| `max_train_steps` | 学習step数 |
| `learning_rate` | 学習率。既定値0.0001 |

## 状態確認と注意

```sh
./gpu-run runs list
./gpu-run status <run-id>
```

ログはrunディレクトリの `stdout.log`、`stderr.log`、`status.json` を確認します。private Hugging Face assetのsecret注入、`--detach`、途中runへのattach/recoverは未実装です。
