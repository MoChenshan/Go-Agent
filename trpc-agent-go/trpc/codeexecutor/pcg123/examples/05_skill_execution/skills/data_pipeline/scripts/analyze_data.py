#!/usr/bin/env python3
"""Data analysis: statistical analysis and report generation"""
import argparse
import csv
import json
import os
from collections import defaultdict, Counter
from datetime import datetime

def analyze_data(input_path, report_path, stats_path, correlation_path):
    """Analyze data and generate reports"""
    data = []
    with open(input_path, 'r', encoding='utf-8') as f:
        reader = csv.DictReader(f)
        data = list(reader)

    stats = {
        'total_records': len(data),
        'unique_users': len(set(d['user_id'] for d in data)),
        'date_range': {
            'start': min(d['timestamp'] for d in data),
            'end': max(d['timestamp'] for d in data)
        }
    }

    action_counts = Counter(d['action'] for d in data)
    category_counts = Counter(d['category'] for d in data)

    stats['action_distribution'] = dict(action_counts)
    stats['category_distribution'] = dict(category_counts)

    value_by_category = defaultdict(list)
    for d in data:
        try:
            value_by_category[d['category']].append(float(d['value']))
        except ValueError:
            pass

    stats['value_statistics_by_category'] = {}
    for category, values in value_by_category.items():
        stats['value_statistics_by_category'][category] = {
            'count': len(values),
            'mean': round(sum(values) / len(values), 2),
            'min': round(min(values), 2),
            'max': round(max(values), 2)
        }

    total_value = sum(float(d.get('value', 0) or 0) for d in data if d.get('value'))
    stats['total_value'] = round(total_value, 2)
    stats['average_value'] = round(total_value / len(data), 2)

    report_lines = [
        "=" * 60,
        "DATA ANALYSIS REPORT",
        "=" * 60,
        f"\nGenerated at: {datetime.now().strftime('%Y-%m-%d %H:%M:%S')}",
        f"\n1. Data Overview",
        f"   - Total records: {stats['total_records']}",
        f"   - Unique users: {stats['unique_users']}",
        f"   - Date range: {stats['date_range']['start']} ~ {stats['date_range']['end']}",
        f"\n2. Action Distribution",
    ]

    for action, count in action_counts.most_common():
        percentage = count / len(data) * 100
        report_lines.append(f"   - {action}: {count} ({percentage:.2f}%)")

    report_lines.extend([
        f"\n3. Category Distribution",
    ])

    for category, count in category_counts.most_common():
        percentage = count / len(data) * 100
        report_lines.append(f"   - {category}: {count} ({percentage:.2f}%)")

    report_lines.extend([
        f"\n4. Value Analysis",
        f"   - Total value: {stats['total_value']}",
        f"   - Average value: {stats['average_value']}",
        f"\n5. Category Value Statistics",
    ])

    for category, stat in stats['value_statistics_by_category'].items():
        report_lines.append(f"   - {category}:")
        report_lines.append(f"     Count: {stat['count']}, Mean: {stat['mean']}, Range: [{stat['min']}, {stat['max']}]")

    report_lines.append("\n" + "=" * 60)

    os.makedirs(os.path.dirname(report_path), exist_ok=True)

    with open(report_path, 'w', encoding='utf-8') as f:
        f.write('\n'.join(report_lines))

    with open(stats_path, 'w', encoding='utf-8') as f:
        json.dump(stats, f, indent=2, ensure_ascii=False)

    correlation_data = [['Category', 'Count', 'Total_Value', 'Avg_Value']]
    for category, stat in stats['value_statistics_by_category'].items():
        count = stat['count']
        total = stat['mean'] * count
        correlation_data.append([category, count, round(total, 2), stat['mean']])

    with open(correlation_path, 'w', newline='', encoding='utf-8') as f:
        writer = csv.writer(f)
        writer.writerows(correlation_data)

    print(f"[OK] Data analysis completed")
    print(f"   - Analysis report: {report_path}")
    print(f"   - Statistics: {stats_path}")
    print(f"   - Correlation data: {correlation_path}")

def main():
    parser = argparse.ArgumentParser(description='Analyze data')
    parser.add_argument('--input', type=str, required=True, help='Input file path')
    parser.add_argument('--report', type=str, default='out/analysis_report.txt', help='Report file path')
    parser.add_argument('--stats', type=str, default='out/stats.json', help='Statistics file path')
    parser.add_argument('--correlation', type=str, default='out/correlation_matrix.csv', help='Correlation file path')
    args = parser.parse_args()

    analyze_data(args.input, args.report, args.stats, args.correlation)

if __name__ == '__main__':
    main()
