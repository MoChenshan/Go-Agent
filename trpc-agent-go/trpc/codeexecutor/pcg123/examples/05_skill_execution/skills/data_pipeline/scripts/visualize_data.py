#!/usr/bin/env python3
"""Data visualization: generate various charts"""
import argparse
import csv
import json
import os
import matplotlib
matplotlib.use('Agg')
import matplotlib.pyplot as plt
import pandas as pd
from collections import Counter

def load_config(config_path):
    """Load configuration file"""
    with open(config_path, 'r', encoding='utf-8') as f:
        return json.load(f)

def create_distribution_plot(data, config, output_path):
    """Create data distribution plots"""
    fig, axes = plt.subplots(2, 2, figsize=config.get('figsize', [12, 10]))

    df = pd.DataFrame(data)

    ax1 = axes[0, 0]
    if 'action' in df.columns:
        action_counts = df['action'].value_counts()
        ax1.bar(range(len(action_counts)), action_counts.values, color='skyblue')
        ax1.set_xticks(range(len(action_counts)))
        ax1.set_xticklabels(action_counts.index, rotation=45, ha='right')
        ax1.set_title('Action Distribution', fontsize=12, fontweight='bold')
        ax1.set_ylabel('Count')

    ax2 = axes[0, 1]
    if 'category' in df.columns:
        category_counts = df['category'].value_counts()
        ax2.pie(category_counts.values, labels=category_counts.index, autopct='%1.1f%%', startangle=90)
        ax2.set_title('Category Distribution', fontsize=12, fontweight='bold')

    ax3 = axes[1, 0]
    if 'value' in df.columns:
        values = pd.to_numeric(df['value'], errors='coerce').dropna()
        ax3.hist(values, bins=30, color='lightcoral', edgecolor='black', alpha=0.7)
        ax3.set_title('Value Distribution', fontsize=12, fontweight='bold')
        ax3.set_xlabel('Value')
        ax3.set_ylabel('Frequency')

    ax4 = axes[1, 1]
    if 'user_id' in df.columns:
        user_counts = df['user_id'].value_counts().head(10)
        ax4.barh(range(len(user_counts)), user_counts.values, color='lightgreen')
        ax4.set_yticks(range(len(user_counts)))
        ax4.set_yticklabels(user_counts.index)
        ax4.set_title('Top 10 Active Users', fontsize=12, fontweight='bold')
        ax4.set_xlabel('Action Count')

    plt.tight_layout()
    plt.savefig(output_path, dpi=config.get('dpi', 100), bbox_inches='tight')
    plt.close()
    print(f"   [OK] Generated distribution plot: {output_path}")

def create_trend_plot(data, config, output_path):
    """Create trend plots"""
    df = pd.DataFrame(data)
    df['timestamp'] = pd.to_datetime(df['timestamp'])
    df['date'] = df['timestamp'].dt.date

    fig, axes = plt.subplots(2, 1, figsize=config.get('figsize', [12, 10]))

    ax1 = axes[0]
    daily_counts = df.groupby('date').size()
    ax1.plot(range(len(daily_counts)), daily_counts.values, marker='o', linestyle='-', linewidth=2, markersize=4)
    ax1.set_xticks(range(0, len(daily_counts), max(1, len(daily_counts)//10)))
    ax1.set_xticklabels([d.strftime('%m-%d') for d in daily_counts.index[::max(1, len(daily_counts)//10)]], rotation=45)
    ax1.set_title('Daily Action Trend', fontsize=12, fontweight='bold')
    ax1.set_ylabel('Action Count')
    ax1.grid(True, alpha=0.3)

    ax2 = axes[1]
    daily_value = df.groupby('date')['value'].apply(lambda x: pd.to_numeric(x, errors='coerce').sum())
    ax2.fill_between(range(len(daily_value)), daily_value.values, alpha=0.3, color='skyblue')
    ax2.plot(range(len(daily_value)), daily_value.values, linewidth=2, color='blue')
    ax2.set_xticks(range(0, len(daily_value), max(1, len(daily_value)//10)))
    ax2.set_xticklabels([d.strftime('%m-%d') for d in daily_value.index[::max(1, len(daily_value)//10)]], rotation=45)
    ax2.set_title('Daily Value Trend', fontsize=12, fontweight='bold')
    ax2.set_ylabel('Total Value')
    ax2.grid(True, alpha=0.3)

    plt.tight_layout()
    plt.savefig(output_path, dpi=config.get('dpi', 100), bbox_inches='tight')
    plt.close()
    print(f"   [OK] Generated trend plot: {output_path}")

def create_correlation_heatmap(data, config, output_path):
    """Create correlation heatmap"""
    df = pd.DataFrame(data)

    action_category = pd.crosstab(df['action'], df['category'])

    fig, ax = plt.subplots(figsize=config.get('figsize', [10, 8]))

    im = ax.imshow(action_category.values, cmap='YlOrRd', aspect='auto')

    ax.set_xticks(range(action_category.shape[1]))
    ax.set_yticks(range(action_category.shape[0]))
    ax.set_xticklabels(action_category.columns, rotation=45, ha='right')
    ax.set_yticklabels(action_category.index)

    for i in range(action_category.shape[0]):
        for j in range(action_category.shape[1]):
            text = ax.text(j, i, action_category.values[i, j],
                          ha="center", va="center", color="black", fontsize=9)

    cbar = plt.colorbar(im, ax=ax)
    cbar.set_label('Count')

    ax.set_title('Action-Category Correlation Heatmap', fontsize=12, fontweight='bold')
    ax.set_xlabel('Category')
    ax.set_ylabel('Action')

    plt.tight_layout()
    plt.savefig(output_path, dpi=config.get('dpi', 100), bbox_inches='tight')
    plt.close()
    print(f"   [OK] Generated correlation heatmap: {output_path}")

def create_box_plot(data, config, output_path):
    """Create box plot"""
    df = pd.DataFrame(data)
    df['value'] = pd.to_numeric(df['value'], errors='coerce')

    categories = df['category'].unique()

    fig, ax = plt.subplots(figsize=config.get('figsize', [12, 6]))

    data_by_category = [df[df['category'] == cat]['value'].dropna() for cat in categories]

    bp = ax.boxplot(data_by_category, labels=categories, patch_artist=True, showmeans=True)

    for patch, color in zip(bp['boxes'], ['lightblue', 'lightcoral', 'lightgreen', 'lightsalmon', 'plum']):
        patch.set_facecolor(color)

    ax.set_title('Value Distribution by Category', fontsize=12, fontweight='bold')
    ax.set_xlabel('Category')
    ax.set_ylabel('Value')
    ax.grid(True, alpha=0.3, axis='y')

    plt.xticks(rotation=45, ha='right')
    plt.tight_layout()
    plt.savefig(output_path, dpi=config.get('dpi', 100), bbox_inches='tight')
    plt.close()
    print(f"   [OK] Generated box plot: {output_path}")

def main():
    parser = argparse.ArgumentParser(description='Visualize data')
    parser.add_argument('--input', type=str, required=True, help='Input file path')
    parser.add_argument('--config', type=str, help='Configuration file path')
    parser.add_argument('--output', type=str, default='out/plots/', help='Output directory')
    args = parser.parse_args()

    default_config = {
        'figsize': [12, 8],
        'dpi': 150,
        'style': 'whitegrid'
    }

    config = default_config
    if args.config and os.path.exists(args.config):
        config = load_config(args.config)
        print(f"[OK] Loaded configuration file: {args.config}")

    data = []
    with open(args.input, 'r', encoding='utf-8') as f:
        reader = csv.DictReader(f)
        data = list(reader)

    os.makedirs(args.output, exist_ok=True)

    print(f"[INFO] Starting visualization generation...")

    create_distribution_plot(data, config, os.path.join(args.output, 'distribution.png'))
    create_trend_plot(data, config, os.path.join(args.output, 'trend.png'))
    create_correlation_heatmap(data, config, os.path.join(args.output, 'correlation_heatmap.png'))
    create_box_plot(data, config, os.path.join(args.output, 'box_plot.png'))

    print(f"[OK] All plots have been generated to directory: {args.output}")

if __name__ == '__main__':
    main()
