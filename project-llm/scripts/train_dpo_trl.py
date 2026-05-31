"""
train_dpo_trl.py —— TRL 原生 DPO 训练（备用路径，比 LLaMAFactory 更灵活）

适用场景：
    - 需要自定义 loss / beta schedule / SimPO-γ 等超参
    - 想在训练过程中注入自定义 callback（如每 N 步调用 reward 观测）

使用：
    python scripts/train_dpo_trl.py \\
        --model_name_or_path ./output/npc_sft_merged \\
        --dataset ./data/processed/npc_dpo.json \\
        --output_dir ./output/npc_dpo_trl \\
        --beta 0.1 --loss_type sigmoid
"""
from __future__ import annotations

import argparse


def main():
    parser = argparse.ArgumentParser(description="TRL 原生 DPO 训练")
    parser.add_argument("--model_name_or_path", type=str, required=True)
    parser.add_argument("--dataset", type=str, required=True)
    parser.add_argument("--output_dir", type=str, required=True)
    parser.add_argument("--beta", type=float, default=0.1)
    parser.add_argument("--loss_type", type=str, default="sigmoid",
                        choices=["sigmoid", "hinge", "ipo", "simpo", "orpo"])
    parser.add_argument("--learning_rate", type=float, default=5e-6)
    parser.add_argument("--num_train_epochs", type=int, default=2)
    parser.add_argument("--per_device_train_batch_size", type=int, default=4)
    parser.add_argument("--gradient_accumulation_steps", type=int, default=4)
    parser.add_argument("--max_length", type=int, default=2048)
    parser.add_argument("--max_prompt_length", type=int, default=1024)
    args = parser.parse_args()

    # TODO(phase-3)：
    # from trl import DPOConfig, DPOTrainer
    # from transformers import AutoTokenizer, AutoModelForCausalLM, BitsAndBytesConfig
    # from datasets import load_dataset
    # from peft import LoraConfig
    #
    # bnb = BitsAndBytesConfig(load_in_4bit=True, bnb_4bit_quant_type="nf4",
    #                          bnb_4bit_use_double_quant=True)
    # model = AutoModelForCausalLM.from_pretrained(args.model_name_or_path,
    #                                              quantization_config=bnb,
    #                                              trust_remote_code=True)
    # tokenizer = AutoTokenizer.from_pretrained(args.model_name_or_path,
    #                                           trust_remote_code=True)
    # ds = load_dataset("json", data_files=args.dataset, split="train")
    # peft_cfg = LoraConfig(r=16, lora_alpha=32, target_modules="all-linear",
    #                       task_type="CAUSAL_LM", use_rslora=True)
    # cfg = DPOConfig(output_dir=args.output_dir, beta=args.beta,
    #                 loss_type=args.loss_type, ...)
    # trainer = DPOTrainer(model=model, args=cfg, train_dataset=ds,
    #                      tokenizer=tokenizer, peft_config=peft_cfg)
    # trainer.train()
    raise NotImplementedError("TODO(phase-3)：TRL DPOTrainer 接入")


if __name__ == "__main__":
    main()
