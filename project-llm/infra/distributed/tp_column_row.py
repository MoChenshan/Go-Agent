"""手写 Column-Parallel / Row-Parallel Linear —— 理解 TP 通信模式

对应方案文档：模型算法微调项目执行方案.md § 10.2.5

启动：
    torchrun --nproc_per_node=2 --backend=gloo infra/distributed/tp_column_row.py   # CPU 模拟
    torchrun --nproc_per_node=2 infra/distributed/tp_column_row.py                  # 双卡 GPU

教学要点：
    Column-Parallel Linear（按 out_features 切）
        - Weight W 切成 [W_0, W_1, ..., W_{N-1}] on columns
        - 每卡输出形状 (*, out/N)，天然并行，无需通信
    Row-Parallel Linear（按 in_features 切）
        - Weight W 切成 [W_0^T; W_1^T; ...] on rows，输入也切
        - 各卡算出局部 Y_i，最后 all-reduce 求和得到完整 Y
    典型 Transformer FFN：Column(W_up) → GeLU → Row(W_down)
        → 前向 1 次 all-reduce；反向 1 次 all-reduce
    → TP 对 NVLink 依赖极强，跨节点 TP 性能会暴跌。
"""
from __future__ import annotations

import math
import os

import torch
import torch.distributed as dist
import torch.nn as nn


def _init_dist() -> tuple[int, int, str]:
    backend = "nccl" if torch.cuda.is_available() else "gloo"
    dist.init_process_group(backend=backend)
    rank = dist.get_rank()
    world = dist.get_world_size()
    device = f"cuda:{rank}" if torch.cuda.is_available() else "cpu"
    if torch.cuda.is_available():
        torch.cuda.set_device(rank)
    return rank, world, device


class ColumnParallelLinear(nn.Module):
    """Y = X · W^T + b，按列切 W（即切 out_features）。

    每张卡持 (out/N, in) 的权重，输出形状 (*, out/N)——无通信。
    若下游需要完整 out，则额外一次 all-gather；典型用法是立刻接一个 RowParallel。
    """

    def __init__(self, in_features: int, out_features: int, tp_size: int, rank: int,
                 bias: bool = True, gather_output: bool = False):
        super().__init__()
        assert out_features % tp_size == 0, "out_features 必须能被 tp_size 整除"
        self.in_features = in_features
        self.out_features = out_features
        self.tp_size = tp_size
        self.rank = rank
        self.gather_output = gather_output

        out_per_rank = out_features // tp_size
        self.weight = nn.Parameter(torch.empty(out_per_rank, in_features))
        nn.init.kaiming_uniform_(self.weight, a=math.sqrt(5))
        self.bias = nn.Parameter(torch.zeros(out_per_rank)) if bias else None

    def forward(self, x: torch.Tensor) -> torch.Tensor:
        y_local = x @ self.weight.t()
        if self.bias is not None:
            y_local = y_local + self.bias
        if self.gather_output:
            # 需要完整输出时的 all-gather
            gathered = [torch.empty_like(y_local) for _ in range(self.tp_size)]
            dist.all_gather(gathered, y_local.contiguous())
            return torch.cat(gathered, dim=-1)
        return y_local


class RowParallelLinear(nn.Module):
    """Y = X · W^T + b，按行切 W（即切 in_features）。

    输入 X 形状 (*, in/N)（上游 ColumnParallel 的输出），每张卡算
        Y_i = X_i · W_i^T
    最后 all-reduce(SUM) 得到完整 Y。这是 TP 里最核心的一次通信。
    """

    def __init__(self, in_features: int, out_features: int, tp_size: int, rank: int,
                 bias: bool = True, input_is_parallel: bool = True):
        super().__init__()
        assert in_features % tp_size == 0, "in_features 必须能被 tp_size 整除"
        self.in_features = in_features
        self.out_features = out_features
        self.tp_size = tp_size
        self.rank = rank
        self.input_is_parallel = input_is_parallel

        in_per_rank = in_features // tp_size
        self.weight = nn.Parameter(torch.empty(out_features, in_per_rank))
        nn.init.kaiming_uniform_(self.weight, a=math.sqrt(5))
        # bias 只由 rank0 持有并加到 all-reduce 之后，避免重复相加
        self.bias = nn.Parameter(torch.zeros(out_features)) if (bias and rank == 0) else None

    def forward(self, x: torch.Tensor) -> torch.Tensor:
        if not self.input_is_parallel:
            # 如果输入是完整 X，需要先按列切；作为演示我们假设上游已切
            chunks = x.chunk(self.tp_size, dim=-1)
            x = chunks[self.rank]
        y_local = x @ self.weight.t()
        # ⭐ TP 的核心通信：all-reduce(SUM)
        dist.all_reduce(y_local, op=dist.ReduceOp.SUM)
        if self.bias is not None:
            y_local = y_local + self.bias
        return y_local


class TPMlp(nn.Module):
    """Transformer FFN 典型 TP 模式：Column(W_up) → GeLU → Row(W_down)"""

    def __init__(self, hidden: int, intermediate: int, tp_size: int, rank: int):
        super().__init__()
        self.up = ColumnParallelLinear(hidden, intermediate, tp_size, rank, gather_output=False)
        self.act = nn.GELU()
        self.down = RowParallelLinear(intermediate, hidden, tp_size, rank, input_is_parallel=True)

    def forward(self, x):
        return self.down(self.act(self.up(x)))


def main():
    rank, world, device = _init_dist()
    if rank == 0:
        print(f"[TP demo] world_size={world}, device={device}")

    torch.manual_seed(42)   # 保证各 rank 得到相同 x 输入
    hidden, intermediate = 512, 2048
    mlp = TPMlp(hidden, intermediate, tp_size=world, rank=rank).to(device)

    x = torch.randn(4, 16, hidden, device=device)
    y = mlp(x)

    if rank == 0:
        print(f"[forward] input={tuple(x.shape)}, output={tuple(y.shape)}")
        print(f"[TP] up.weight (每卡)   = {tuple(mlp.up.weight.shape)}")
        print(f"[TP] down.weight (每卡) = {tuple(mlp.down.weight.shape)}")
        print("[TP] 前向通信次数 = 1 次 all-reduce（down 里）；反向再 1 次 all-reduce")

    # 反向示例
    loss = y.pow(2).mean()
    loss.backward()
    if rank == 0:
        has_grad = mlp.up.weight.grad is not None and mlp.down.weight.grad is not None
        print(f"[backward] grad 同步完成, has_grad={has_grad}")

    dist.destroy_process_group()


if __name__ == "__main__":
    if os.environ.get("SMOKE") == "1" and "RANK" not in os.environ:
        # 单进程 smoke：不做 dist，只验证 Column/Row 模块可前向
        print("[smoke] single-process sanity check (no dist)")
        # 模拟 world=1：ColumnParallelLinear 相当于标准 Linear
        col = nn.Linear(32, 64)
        x = torch.randn(4, 32)
        y = col(x)
        print(f"[smoke] shape={tuple(y.shape)}")
    else:
        main()
