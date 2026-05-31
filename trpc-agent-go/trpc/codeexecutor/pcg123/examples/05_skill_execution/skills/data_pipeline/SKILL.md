---
name: data-pipeline
description: 数据流水线分析技能 - 用于演示数据分析流水线的完整流程
---

## 概述

这是一个高级数据流水线分析演示技能，展示框架的核心能力：
- **多脚本数据交互**：不同脚本之间通过输入输出文件传递数据
- **Artifact 管理**：从 artifact 仓库加载历史数据，保存分析结果
- **Workspace 输入**：从工作区读取之前步骤的输出作为下一步的输入
- **Host 输入**：从用户本地文件系统复制数据到工作区（适合已有本地数据集的场景）
- **Skill 资源文件**：从 skill 目录读取模板配置文件
- **声明式输出收集**：使用 glob 模式自动收集多个输出文件
- **完整数据流程**：数据生成 → 清洗 → 分析 → 可视化

## 工作流程

```
Step 1: generate_data.py（或通过 host:// 直接使用本地已有数据）
   ↓ (生成/导入原始数据到 out/)
Step 2: clean_data.py
   ↓ (读取 out/raw_data.csv, 清洗后输出 out/clean_data.csv)
Step 3: analyze_data.py
   ↓ (读取 out/clean_data.csv, 生成分析报告到 out/)
Step 4: visualize_data.py
   ↓ (读取 out/clean_data.csv, 生成可视化图表)
Final: 收集所有输出文件
```

## 命令示例

### 完整流水线：端到端数据分析

```bash
# Step 1: 生成模拟数据（也可用 host:// 导入本地已有数据，跳过此步）
skill_run(command="python3 scripts/generate_data.py --rows 1000 --output out/raw_data.csv") \
  outputs=["out/raw_data.csv"]
# Step 2: 数据清洗（从 workspace 读取 Step 1 的输出）
skill_run(command="python3 scripts/clean_data.py --input work/inputs/raw_data.csv --output out/clean_data.csv") \
  inputs=[{"from": "workspace://out/raw_data.csv"}] \
  outputs=["out/clean_data.csv"]

# Step 3: 数据分析（从 workspace 读取清洗后的数据）
skill_run(command="python3 scripts/analyze_data.py --input work/inputs/clean_data.csv --report out/analysis_report.txt") \
  inputs=[{"from": "workspace://out/clean_data.csv"}] \
  outputs=["out/analysis_report.txt", "out/stats.json"]

# Step 4: 数据可视化（读取清洗数据 + skill 模板）
skill_run(command="python3 scripts/visualize_data.py --input work/inputs/clean_data.csv --config work/inputs/plot_config.json --output out/plots/") \
  inputs=[
    {"from": "workspace://out/clean_data.csv"},
    {"from": "skill://data-pipeline/templates/plot_config.json"}
  ] \
  outputs=["out/plots/*.png"]

# Step 5: 生成最终报告（收集所有输出）, 保存为制品
skill_run(command="python3 scripts/generate_report.py --input work/results/ --output out/final_report.txt") \
  inputs=[{"from": "workspace://out/", "to": "work/results/"}] \
  outputs=["out/final_report.txt"]
  save_artifacts=true
```

### 单独执行各个步骤

#### 1. 生成或导入原始数据

**方式 A**：生成模拟数据集：

```bash
python3 scripts/generate_data.py --rows 1000 --output out/raw_data.csv
```

**方式 B**：如果本地已有数据文件，可通过 `host://` 直接导入到工作区：

```bash
skill_run(command="python3 scripts/clean_data.py --input work/inputs/raw_data.csv --output out/clean_data.csv") \
  inputs=[{"from": "host:///path/to/local/raw_data.csv"}] \
  outputs=["out/clean_data.csv"]
```

**输出文件**：
- `out/raw_data.csv` - 原始数据（包含用户ID、时间戳、行为类型、数值等字段）

#### 2. 数据清洗

清洗原始数据，处理缺失值和异常值：

```bash
# 使用 workspace 输入（上一步的输出）
python3 scripts/clean_data.py --input work/inputs/raw_data.csv --output out/clean_data.csv
```

**输入文件**（通过 inputs 映射，支持多种来源）：
- `workspace://out/raw_data.csv` → `work/inputs/raw_data.csv`（从工作区映射）
- `host:///path/to/local/data.csv` → `work/inputs/data.csv`（从本地文件系统复制）

**输出文件**：
- `out/clean_data.csv` - 清洗后的数据

#### 3. 数据分析

对清洗后的数据进行统计分析：

```bash
python3 scripts/analyze_data.py --input work/inputs/clean_data.csv --report out/analysis_report.txt
```

**输入文件**（通过 inputs 映射）：
- `workspace://out/clean_data.csv` → `work/inputs/clean_data.csv`（自动映射）

**输出文件**：
- `out/analysis_report.txt` - 分析报告（文本格式）
- `out/stats.json` - 统计数据（JSON 格式）

#### 4. 数据可视化

生成多种可视化图表：

```bash
# 使用 skill 资源文件作为配置
python3 scripts/visualize_data.py \
  --input work/inputs/clean_data.csv \
  --config work/inputs/plot_config.json \
  --output out/plots/
```

**输入文件**（通过 inputs 映射）：
- `workspace://out/clean_data.csv` → `work/inputs/clean_data.csv`（自动映射）
- `skill://data-pipeline/templates/plot_config.json` → `work/inputs/plot_config.json`（自动映射）

**输出文件**（使用 glob 收集）：
- `out/plots/distribution.png` - 数据分布图
- `out/plots/trend.png` - 趋势图
- `out/plots/correlation_heatmap.png` - 相关性热力图
- `out/plots/box_plot.png` - 箱线图

#### 5. 生成最终报告

整合所有分析结果生成报告：

```bash
python3 scripts/generate_report.py --input work/inputs/ --output out/final_report.txt
```

**输入文件**（通过 inputs 映射）：
- `workspace://out` → `work/inputs/`（整个目录）

**输出文件**：
- `out/final_report.txt` - 最终分析报告

## 输出文件说明

- `out/raw_data.csv` - 原始生成的数据
- `out/clean_data.csv` - 清洗后的数据
- `out/analysis_report.txt` - 文本格式的分析报告
- `out/stats.json` - JSON 格式的统计数据
- `out/plots/` - 所有可视化图表（PNG 格式）
- `out/final_report.txt` - 最终整合的报告