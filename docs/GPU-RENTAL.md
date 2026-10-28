# Cheap GPU Rental Guide: Persistent Storage for LLMs

A step-by-step guide to renting cheap cloud GPUs (under $0.25/hour) and keeping your downloaded models saved permanently for ~$1.50 to $3.00/month when the GPU is shut down.

---

## 1. Cost Overview

| Item | Cost | When You Pay |
|---|---|---|
| **GPU Compute (RTX 3090 24GB)** | ~$0.20 - $0.24 / hour | Only when the pod is **RUNNING** |
| **Network Volume Storage (40 GB)** | ~$2.80 / month ($0.07/GB) | Always (keeps model safe forever) |
| **GPU Compute when STOPPED** | **$0.00 / hour** | When pod is stopped or terminated |

With a $10 deposit, you can store your model for 3 months and still run ~20 hours of active GPU time.

---

## 2. Top 3 Cheap Providers

1. **RunPod (Recommended)**
   - **Storage style**: Detachable Network Volume.
   - **Advantage**: You can completely delete (terminate) the GPU pod to pay $0/hour. The volume stays in the cloud data center ready to attach to any new GPU.
   - **Typical GPU**: RTX 3090 ($0.22/hr) or RTX 4090 ($0.34/hr).

2. **TensorDock**
   - **Storage style**: Persistent Cloud SSD attached to VM.
   - **Advantage**: Very low hourly rates ($0.19/hr for RTX 3090).
   - **Drawback**: Fewer templates; requires basic Linux command line setup.

3. **Vast.ai**
   - **Storage style**: Host-specific stopped disk.
   - **Advantage**: Lowest starting prices ($0.15/hr).
   - **Drawback**: Hosted on community home/office machines. If the host machine goes offline, your instance cannot start until the host returns.

---

## 3. Step-by-Step Setup on RunPod

### Step 1: Create a Persistent Network Volume (2 minutes)
1. Log in to [RunPod Console](https://www.runpod.io/console/storage).
2. Click **Network Volumes** → **Add Volume**.
3. Choose a data center region (e.g. `US-NJ` or `EU-RO-1`).
4. Set size to **40 GB** (fits multiple 27B/30B GGUF models).
5. Click **Create**.

### Step 2: Deploy an On-Demand GPU (1 minute)
1. Go to **Pods** → **Deploy Pod**.
2. Under **Community Cloud**, find **RTX 3090** (24 GB VRAM) or **RTX 4090**.
3. Select template: `RunPod Ollama` or `RunPod Fast Stable Diffusion / LLM`.
4. In **Network Volume**, select the 40 GB volume created in Step 1 and mount path `/workspace`.
5. Click **Continue** → **Deploy**.

### Step 3: Download Your Model Once (3 to 5 minutes)
1. Open the Pod **Web Terminal** (or connect via SSH).
2. Move into the persistent volume folder:
   ```bash
   cd /workspace
   ```
3. Download the model (example: Qwen3.8-27B Uncensored GGUF):
   ```bash
   wget -c https://huggingface.co/HauhauCS/Qwen3.8-27B-Uncensored-HauhauCS-Aggressive-MTP-GGUF/resolve/main/Qwen3.8-27B-Uncensored-HauhauCS-Aggressive-IQ2_M.gguf
   ```
4. Verify the file exists:
   ```bash
   ls -lh /workspace
   ```

### Step 4: Run the Model as an API Server (1 minute)
Launch `llama-server` (or Ollama) listening on port 8000:
```bash
llama-server \
  --model /workspace/Qwen3.8-27B-Uncensored-HauhauCS-Aggressive-IQ2_M.gguf \
  --port 8000 \
  --host 0.0.0.0 \
  --ctx-size 8192 \
  --n-gpu-layers 99
```

---

## 4. How to Shut Down Without Losing Files (30 seconds)

When you are done using the model:
1. Go to the RunPod **Pods** page.
2. Click **Terminate Pod** (the red trash can icon).
3. Confirm termination.
4. **Result**: Your hourly GPU billing drops to **$0.00**. Your model file remains untouched in your Network Volume.

To use it again tomorrow or next month:
1. Deploy a new pod in the **same region**.
2. Mount your existing Network Volume to `/workspace`.
3. Your downloaded model is immediately available. No downloading needed.

---

## 5. Connect to CLIProxyAPI

Point CLIProxyAPI directly to your running RunPod instance in `config.yaml`:

```yaml
upstream:
  openai-compatibility:
    - name: "runpod-qwen-uncensored"
      base-url: "https://<YOUR_RUNPOD_POD_ID>-8000.proxy.runpod.net/v1"
      keys:
        - api-key: "none"
      models:
        - name: "Qwen3.8-27B-Uncensored"
          alias: "qwen-uncensored"
```
