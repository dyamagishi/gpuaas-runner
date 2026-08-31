# Example: Anima LoRA on RunPod

`recipes/anima-lora.yaml` runs `anima_train_network.py` / `networks.lora_anima` from sd-scripts **v0.11.1**. It is a separate image from the SDXL sample (`Dockerfile.anima`). Do not point `recipes/sdxl-lora.yaml` at Anima.

CircleStone Anima weights are **non-commercial**. The trained LoRA inherits that license.

## Dataset

Local directory with exactly these four children (png + same-name `.txt`):

```text
dataset/train/
├── color_3x/
├── emblem_1x/
├── manga_plain_1x/
└── rough_1x/
```

The image helper writes a run-local `dataset.toml`. Do not edit the tamarindus canonical toml.

## Config

`~/.config/gpu-run/config.yaml` `max_runtime` is the whole Pod lifetime. 1800s is not enough for a full 1800-step run. Set at least `14400s`. Keep `allowed_gpu_ids: [NVIDIA A40]`. `--detach` is unimplemented; run in tmux.

## Models

Pin Hugging Face `resolve` URLs to a commit, not `main`. Current pin used by macxes paint v3:

```text
https://huggingface.co/circlestone-labs/Anima/resolve/f973fc41ec7545364ac9776c2440285f43ff2a30/split_files/diffusion_models/anima-base-v1.0.safetensors
https://huggingface.co/circlestone-labs/Anima/resolve/f973fc41ec7545364ac9776c2440285f43ff2a30/split_files/text_encoders/qwen_3_06b_base.safetensors
https://huggingface.co/circlestone-labs/Anima/resolve/f973fc41ec7545364ac9776c2440285f43ff2a30/split_files/vae/qwen_image_vae.safetensors
```

## Commands

Replace the zero digest in `recipes/anima-lora.yaml` with the published `gpu-run-anima-sd-scripts` digest first.

```sh
ANIMA_DIT='https://huggingface.co/circlestone-labs/Anima/resolve/f973fc41ec7545364ac9776c2440285f43ff2a30/split_files/diffusion_models/anima-base-v1.0.safetensors'
QWEN3='https://huggingface.co/circlestone-labs/Anima/resolve/f973fc41ec7545364ac9776c2440285f43ff2a30/split_files/text_encoders/qwen_3_06b_base.safetensors'
VAE='https://huggingface.co/circlestone-labs/Anima/resolve/f973fc41ec7545364ac9776c2440285f43ff2a30/split_files/vae/qwen_image_vae.safetensors'
DATASET=/Users/daiki/projects/macxes/anima-macxes-paint-lora-v3/dataset/train

./gpu-run recipe validate recipes/anima-lora.yaml \
  --input dataset_dir="$DATASET" \
  --input anima_dit="$ANIMA_DIT" \
  --input qwen3="$QWEN3" \
  --input vae="$VAE" \
  --input output_name=anima_macxes_paint_v3 \
  --input max_train_steps=1

./gpu-run run recipes/anima-lora.yaml \
  --input dataset_dir="$DATASET" \
  --input anima_dit="$ANIMA_DIT" \
  --input qwen3="$QWEN3" \
  --input vae="$VAE" \
  --input output_name=anima_macxes_paint_v3 \
  --input max_train_steps=1
```

Full 1800 uses the same command with `max_train_steps=1800`. Artifacts land in `runs/anima-lora/<run-id>/checkpoints/`. Copy surviving 600/1200/1800 to tamarindus `anima/candidates/` only; do not overwrite the live Comfy v3 slot or v2.
