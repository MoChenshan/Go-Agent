#!/usr/bin/env python3
"""Generate final report (simplified version, text report)"""
import argparse
import os
import json
from datetime import datetime

def generate_report(input_dir, output_path):
    """Generate integrated report"""

    report_lines = [
        "=" * 80,
        "DATA PIPELINE ANALYSIS - FINAL REPORT",
        "=" * 80,
        f"\nGenerated at: {datetime.now().strftime('%Y-%m-%d %H:%M:%S')}",
        f"\nReport directory: {input_dir}",
        "\n" + "=" * 80,
        "1. Data Processing Pipeline",
        "=" * 80,
        "\n[OK] Step 1: Data Generation",
        "   - Generated synthetic user behavior data",
        "\n[OK] Step 2: Data Cleaning",
        "   - Handled missing values and outliers",
        "   - Improved data quality",
        "\n[OK] Step 3: Data Analysis",
        "   - Statistical analysis",
        "   - Action and category distribution",
        "   - Value analysis",
        "\n[OK] Step 4: Data Visualization",
        "   - Distribution plots",
        "   - Trend plots",
        "   - Correlation heatmap",
        "   - Box plots",
    ]

    analysis_report = os.path.join(input_dir, 'analysis_report.txt')
    if os.path.exists(analysis_report):
        report_lines.extend([
            "\n" + "=" * 80,
            "2. Detailed Analysis Results",
            "=" * 80,
            ""
        ])
        with open(analysis_report, 'r', encoding='utf-8') as f:
            report_lines.extend(f.readlines())

    stats_file = os.path.join(input_dir, 'stats.json')
    if os.path.exists(stats_file):
        report_lines.extend([
            "\n" + "=" * 80,
            "3. Statistical Summary",
            "=" * 80,
            ""
        ])
        with open(stats_file, 'r', encoding='utf-8') as f:
            stats = json.load(f)
            report_lines.append(f"Total records: {stats.get('total_records', 'N/A')}")
            report_lines.append(f"Unique users: {stats.get('unique_users', 'N/A')}")
            report_lines.append(f"Total value: {stats.get('total_value', 'N/A')}")
            report_lines.append(f"Average value: {stats.get('average_value', 'N/A')}")

    plots_dir = os.path.join(input_dir, 'plots')
    if os.path.exists(plots_dir):
        plots = [f for f in os.listdir(plots_dir) if f.endswith('.png')]
        if plots:
            report_lines.extend([
                "\n" + "=" * 80,
                "4. Generated Visualization Charts",
                "=" * 80,
                ""
            ])
            for plot in sorted(plots):
                report_lines.append(f"  [CHART] {plot}")

    report_lines.extend([
        "\n" + "=" * 80,
        "5. Advanced Features Demonstrated",
        "=" * 80,
        "",
        "[OK] Multi-script Data Interaction",
        "   - Each step reads output files from previous steps",
        "   - Data flow via workspace:// protocol",
        "",
        "[OK] File Input Mapping",
        "   - artifact://: Load historical data",
        "   - workspace://: Workspace files",
        "   - skill://: Skill resource files",
        "",
        "[OK] Declarative Output Collection",
        "   - Glob pattern matching: out/plots/*.png",
        "   - Automatic collection of all output files",
        "",
        "[OK] Workspace Management",
        "   - work/: Working directory",
        "   - out/: Output directory",
        "   - skills/: Skill scripts",
        "",
        "=" * 80,
        "END OF REPORT",
        "=" * 80,
    ])

    os.makedirs(os.path.dirname(output_path), exist_ok=True)

    with open(output_path, 'w', encoding='utf-8') as f:
        f.write('\n'.join(report_lines))

    print(f"[OK] Final report generated: {output_path}")
    print(f"   Report contains {len(report_lines)} lines")

def main():
    parser = argparse.ArgumentParser(description='Generate final report')
    parser.add_argument('--input', type=str, required=True, help='Input directory')
    parser.add_argument('--output', type=str, default='out/final_report.txt', help='Output file path')
    args = parser.parse_args()

    generate_report(args.input, args.output)

if __name__ == '__main__':
    main()
