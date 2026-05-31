#!/usr/bin/env python3
"""Data cleaning: handle missing values and outliers"""
import argparse
import csv
import os
from collections import defaultdict
import json

def clean_data(input_path, output_path, log_path):
    """Clean data by removing missing values and outliers"""
    rows = []
    missing_values = defaultdict(int)
    outliers_removed = 0

    with open(input_path, 'r', encoding='utf-8') as f:
        reader = csv.DictReader(f)
        fieldnames = reader.fieldnames

        for row in reader:
            cleaned = True

            for field in fieldnames:
                if row[field] in ['', 'None', 'null']:
                    missing_values[field] += 1
                    cleaned = False
                    continue

            if not cleaned:
                continue

            try:
                value = float(row['value'])
                if value < 0 or value > 1000:
                    outliers_removed += 1
                    continue
            except ValueError:
                missing_values['value'] += 1
                continue

            rows.append(row)

    os.makedirs(os.path.dirname(output_path), exist_ok=True)

    with open(output_path, 'w', newline='', encoding='utf-8') as f:
        writer = csv.DictWriter(f, fieldnames=fieldnames)
        writer.writeheader()
        writer.writerows(rows)

    log_data = {
        'total_rows': len(rows) + sum(missing_values.values()) + outliers_removed,
        'cleaned_rows': len(rows),
        'missing_values_by_field': dict(missing_values),
        'outliers_removed': outliers_removed,
        'retention_rate': f"{len(rows) / (len(rows) + sum(missing_values.values()) + outliers_removed) * 100:.2f}%"
    }

    with open(log_path, 'w', encoding='utf-8') as f:
        json.dump(log_data, f, indent=2, ensure_ascii=False)

    return log_data

def main():
    parser = argparse.ArgumentParser(description='Clean data')
    parser.add_argument('--input', type=str, required=True, help='Input file path')
    parser.add_argument('--output', type=str, required=True, help='Output file path')
    parser.add_argument('--log', type=str, default=None, help='Log file path')
    args = parser.parse_args()

    log_path = args.log or args.output.replace('.csv', '_cleaning_log.json')
    log_data = clean_data(args.input, args.output, log_path)

    print(f"[OK] Data cleaning completed")
    print(f"   - Original rows: {log_data['total_rows']}")
    print(f"   - Cleaned rows: {log_data['cleaned_rows']}")
    print(f"   - Retention rate: {log_data['retention_rate']}")
    print(f"   - Outliers removed: {log_data['outliers_removed']}")
    print(f"   - Output file: {args.output}")

if __name__ == '__main__':
    main()
