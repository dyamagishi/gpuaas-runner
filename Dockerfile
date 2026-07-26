ARG CUDA_BASE_IMAGE=nvidia/cuda@sha256:REPLACE_WITH_64_HEX_DIGEST
FROM ${CUDA_BASE_IMAGE}

ARG DEBIAN_FRONTEND=noninteractive
ARG SD_SCRIPTS_COMMIT=37a1cbbc5725ed2a3575506e7bd2001c9908ac92
ARG PYTORCH_VERSION=2.5.1

ENV LANG=C.UTF-8 \
    LC_ALL=C.UTF-8 \
    PIP_NO_CACHE_DIR=1 \
    PYTHONUNBUFFERED=1 \
    GPU_RUN_WORKSPACE=/workspace/gpu-run

RUN apt-get update && apt-get install -y --no-install-recommends \
      ca-certificates git openssh-client openssh-server rsync tini curl python3.10 python3.10-venv \
      python3-pip build-essential libgl1 libglib2.0-0 \
    && rm -rf /var/lib/apt/lists/* \
    && python3.10 -m venv /opt/venv

ENV PATH=/opt/venv/bin:$PATH

RUN python -m pip install --upgrade pip==24.3.1 setuptools==75.6.0 wheel==0.45.1 \
    && python -m pip install \
      torch==${PYTORCH_VERSION} torchvision==0.20.1 --index-url https://download.pytorch.org/whl/cu124 \
    && python -m pip install \
      accelerate==1.2.1 bitsandbytes==0.45.0 diffusers==0.31.0 transformers==4.47.1 \
      safetensors==0.4.5 einops==0.8.0 toml==0.10.2 huggingface-hub==0.27.0 \
      pillow==11.0.0 numpy==2.1.3 opencv-python-headless==4.10.0.84

RUN git clone https://github.com/kohya-ss/sd-scripts.git /opt/sd-scripts \
    && cd /opt/sd-scripts \
    && git checkout --detach ${SD_SCRIPTS_COMMIT} \
    && python -m pip install -r requirements.txt

RUN mkdir -p ${GPU_RUN_WORKSPACE}/runs /opt/gpu-run \
    && printf '%s\n' "${SD_SCRIPTS_COMMIT}" > /opt/sd-scripts/REVISION \
    && printf '%s\n' "${PYTORCH_VERSION}" > /opt/gpu-run/PYTORCH_VERSION \
    && printf '%s\n' "12.4.1" > /opt/gpu-run/CUDA_VERSION

COPY scripts/remote-runner.sh /opt/gpu-run/remote-runner
COPY scripts/entrypoint.sh /opt/gpu-run/entrypoint
COPY scripts/sshd_config /etc/ssh/sshd_config
RUN chmod 0755 /opt/gpu-run/remote-runner /opt/gpu-run/entrypoint \
    && test -s /etc/ssh/sshd_config

WORKDIR ${GPU_RUN_WORKSPACE}
ENTRYPOINT ["/usr/bin/tini", "--"]
CMD ["/opt/gpu-run/entrypoint"]
