# Recipe schema v1

Recipeは信頼できるローカルYAMLとしてGit管理する。形式は [`recipes/schema-v1.json`](../recipes/schema-v1.json) で定義する。unknown field、重複キー、未解決 `${...}`、digestでないimageはCLIが拒否する。

## 固定した入力名と成果物

`sdxl-lora` の入力名は `dataset_dir`（画像と同名 `.txt` captionを同一ディレクトリに置く）、`base_model`（ローカルの展開済みbase-model directory）、`output_name`、`resolution`、`train_batch_size`、`max_train_steps`、`learning_rate`。v1ではremoteへのsecret注入を行わないため、HF private assetは対象外とする。成果物名は必須の `final_model` と任意の `checkpoints` に固定する。

`argv` はshell文字列ではなく配列であり、値はargvの引数として渡す。ローカルパスはremote_pathへ展開され、shell展開・パス traversalは許可しない。

## 制限の優先順位

安全側に固定する。`effective = min(recipe, user config)` を数値上限（disk/runtime/price）に適用し、recipeのGPU要件・許可GPUはconfigの許可集合との積集合にする。CLI optionでconfigのhard limitを緩和できない。recipeより厳しいCLI指定は将来拡張で許可するが、v1では未実装として拒否する。

## 未実装

Vast.ai、musubi-tuner固有recipe、spot bid、multi-GPU、network volume、remote registry、stage再開以外のcheckpoint自動resumeはv1対象外。
