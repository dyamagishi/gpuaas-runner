#!/opt/venv/bin/python
"""One-stage Anima LoRA helper for gpu-run.

Writes a run-local dataset.toml from the transferred four-subset directory,
runs anima_train_network.py, then inspects the final safetensors.
"""
from __future__ import annotations

import argparse
import hashlib
import json
import re
import subprocess
import sys
from pathlib import Path

SUBSETS = (
    ("color_3x", 3, 1),
    ("emblem_1x", 1, 2),
    ("manga_plain_1x", 1, 1),
    ("rough_1x", 1, 1),
)
SD_SCRIPTS = Path("/opt/sd-scripts")
ACCELERATE = Path("/opt/venv/bin/accelerate")


def toml_escape(value: str) -> str:
    return '"' + value.replace("\\", "\\\\").replace('"', '\\"') + '"'


def write_dataset_toml(dataset_dir: Path, destination: Path) -> None:
    if not dataset_dir.is_dir():
        raise SystemExit(f"dataset dir missing: {dataset_dir}")
    blocks = [
        "[general]",
        "resolution = 1536",
        'caption_extension = ".txt"',
        "shuffle_caption = true",
        "keep_tokens = 1",
        "caption_dropout_rate = 0.15",
        "enable_bucket = true",
        "bucket_no_upscale = true",
        "bucket_reso_steps = 64",
        "max_bucket_reso = 2048",
        "",
        "[[datasets]]",
        "resolution = 1536",
        "batch_size = 1",
        "enable_bucket = true",
        "bucket_no_upscale = true",
        "bucket_reso_steps = 64",
        "max_bucket_reso = 2048",
        "",
    ]
    for name, repeats, keep_tokens in SUBSETS:
        image_dir = dataset_dir / name
        if not image_dir.is_dir():
            raise SystemExit(f"missing subset directory: {image_dir}")
        pngs = list(image_dir.glob("*.png"))
        if not pngs:
            raise SystemExit(f"subset has no png files: {image_dir}")
        blocks.extend(
            [
                "  [[datasets.subsets]]",
                f"  image_dir = {toml_escape(str(image_dir.resolve()))}",
                f"  num_repeats = {repeats}",
                '  caption_extension = ".txt"',
                "  shuffle_caption = true",
                f"  keep_tokens = {keep_tokens}",
                "  caption_dropout_rate = 0.15",
                "",
            ]
        )
    destination.parent.mkdir(parents=True, exist_ok=True)
    destination.write_text("\n".join(blocks), encoding="utf-8")


def inspect_lora(path: Path) -> dict:
    import numpy as np
    from safetensors import safe_open

    if not path.is_file() or path.suffix != ".safetensors" or path.stat().st_size == 0:
        raise SystemExit(f"final checkpoint missing or empty: {path}")
    ranks, alphas, keys = set(), set(), []
    nonfinite = 0
    digest = hashlib.sha256()
    with path.open("rb") as handle:
        for chunk in iter(lambda: handle.read(1024 * 1024), b""):
            digest.update(chunk)
    with safe_open(path, framework="np") as handle:
        metadata = handle.metadata() or {}
        for key in handle.keys():
            keys.append(key)
            tensor = handle.get_tensor(key)
            nonfinite += int(tensor.size - np.count_nonzero(np.isfinite(tensor)))
            if key.endswith(".lora_down.weight") and tensor.ndim >= 2:
                ranks.add(int(tensor.shape[0]))
            if key.endswith(".alpha") and tensor.size == 1:
                alphas.add(float(tensor.reshape(-1)[0]))
        for field in ("ss_network_dim", "network_dim"):
            if field in metadata:
                match = re.search(r"\d+", metadata[field])
                if match:
                    ranks.add(int(match.group()))
        for field in ("ss_network_alpha", "network_alpha"):
            if field in metadata:
                match = re.search(r"[0-9.]+", metadata[field])
                if match:
                    alphas.add(float(match.group()))
    te_keys = [key for key in keys if key.startswith("lora_te")]
    report = {
        "path": str(path.resolve()),
        "sha256": digest.hexdigest(),
        "key_count": len(keys),
        "te_key_count": len(te_keys),
        "ranks": sorted(ranks),
        "alphas": sorted(alphas),
        "nonfinite": nonfinite,
    }
    print(json.dumps(report, indent=2))
    if nonfinite or 32 not in ranks or 16.0 not in alphas or not keys or not te_keys:
        raise SystemExit("FAIL: expected TE keys, rank32, alpha16, nonfinite0")
    print("PASS: keys present, TE keys present, rank=32, alpha=16, nonfinite=0")
    return report


def train(args: argparse.Namespace) -> None:
    output_dir = args.output_dir.resolve()
    output_dir.mkdir(parents=True, exist_ok=True)
    log_dir = output_dir / "logs"
    log_dir.mkdir(parents=True, exist_ok=True)
    dataset_toml = output_dir / "dataset.toml"
    write_dataset_toml(args.dataset_dir.resolve(), dataset_toml)
    save_every = min(100, args.max_train_steps)
    argv = [
        str(ACCELERATE),
        "launch",
        "--mixed_precision",
        "bf16",
        "--num_cpu_threads_per_process",
        "1",
        "anima_train_network.py",
        f"--pretrained_model_name_or_path={args.anima_dit}",
        f"--qwen3={args.qwen3}",
        f"--vae={args.vae}",
        f"--dataset_config={dataset_toml}",
        f"--output_dir={output_dir}",
        f"--output_name={args.output_name}",
        "--save_model_as=safetensors",
        "--save_precision=fp16",
        "--mixed_precision=bf16",
        "--network_module=networks.lora_anima",
        "--network_dim=32",
        "--network_alpha=16",
        "--network_args",
        "network_reg_dims=.*self_attn.*=32,.*cross_attn.*=32,.*mlp.*=16",
        "network_reg_lrs=.*self_attn.*=8e-5,.*cross_attn.*=8e-5,.*mlp.*=4e-5",
        "--optimizer_type=AdamW8bit",
        "--learning_rate=8e-5",
        "--text_encoder_lr=4e-5",
        "--lr_scheduler=constant_with_warmup",
        "--lr_warmup_steps=45",
        f"--max_train_steps={args.max_train_steps}",
        f"--save_every_n_steps={save_every}",
        "--train_batch_size=1",
        "--gradient_accumulation_steps=1",
        "--gradient_checkpointing",
        "--cache_latents_to_disk",
        "--vae_chunk_size=64",
        "--vae_disable_cache",
        "--timestep_sampling=sigmoid",
        "--max_data_loader_n_workers=0",
        "--seed=42",
        f"--logging_dir={log_dir}",
    ]
    completed = subprocess.run(argv, cwd=str(SD_SCRIPTS), check=False)
    if completed.returncode != 0:
        raise SystemExit(completed.returncode)
    inspect_lora(output_dir / f"{args.output_name}.safetensors")


def main() -> None:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--dataset-dir", type=Path, required=True)
    parser.add_argument("--anima-dit", type=Path)
    parser.add_argument("--qwen3", type=Path)
    parser.add_argument("--vae", type=Path)
    parser.add_argument("--output-dir", type=Path, required=True)
    parser.add_argument("--output-name", required=True)
    parser.add_argument("--max-train-steps", type=int, default=1800)
    parser.add_argument("--write-toml-only", action="store_true")
    args = parser.parse_args()
    if args.max_train_steps < 1:
        raise SystemExit("max-train-steps must be >= 1")
    if not re.fullmatch(r"[A-Za-z0-9_-]+", args.output_name):
        raise SystemExit("output-name must match [A-Za-z0-9_-]+")
    if args.write_toml_only:
        destination = args.output_dir / "dataset.toml"
        write_dataset_toml(args.dataset_dir.resolve(), destination)
        print(f"PASS: wrote {destination}")
        return
    for label, path in (("anima-dit", args.anima_dit), ("qwen3", args.qwen3), ("vae", args.vae)):
        if path is None or not path.is_file():
            raise SystemExit(f"{label} must be an existing file")
    train(args)


if __name__ == "__main__":
    try:
        main()
    except BrokenPipeError:
        sys.exit(1)
