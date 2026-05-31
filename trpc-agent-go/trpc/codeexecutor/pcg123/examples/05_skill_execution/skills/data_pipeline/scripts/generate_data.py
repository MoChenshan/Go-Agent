#!/usr/bin/env python3
"""Generate synthetic dataset"""
import argparse
import csv
import random
from datetime import datetime, timedelta
import json

def generate_user_data(rows):
    """Generate user behavior data"""
    actions = ['view', 'click', 'purchase', 'add_to_cart', 'remove_from_cart']
    categories = ['electronics', 'clothing', 'books', 'home', 'sports']

    data = []
    base_time = datetime.now() - timedelta(days=30)

    for i in range(rows):
        user_id = f"user_{random.randint(1, 100)}"
        timestamp = base_time + timedelta(
            days=random.randint(0, 30),
            hours=random.randint(0, 23),
            minutes=random.randint(0, 59)
        )
        action = random.choice(actions)
        category = random.choice(categories)

        value = random.uniform(10, 500) if action == 'purchase' else random.uniform(1, 100)

        row = {
            'user_id': user_id,
            'timestamp': timestamp.strftime('%Y-%m-%d %H:%M:%S'),
            'action': action,
            'category': category,
            'value': round(value, 2),
            'rating': random.randint(1, 5) if random.random() > 0.7 else None
        }
        data.append(row)

    return sorted(data, key=lambda x: x['timestamp'])

def main():
    parser = argparse.ArgumentParser(description='Generate synthetic data')
    parser.add_argument('--rows', type=int, default=1000, help='Number of rows to generate')
    parser.add_argument('--output', type=str, required=True, help='Output file path')
    args = parser.parse_args()

    data = generate_user_data(args.rows)

    os.makedirs(os.path.dirname(args.output), exist_ok=True)

    with open(args.output, 'w', newline='', encoding='utf-8') as f:
        writer = csv.DictWriter(f, fieldnames=['user_id', 'timestamp', 'action', 'category', 'value', 'rating'])
        writer.writeheader()
        writer.writerows(data)

    print(f"[OK] Generated {len(data)} rows to {args.output}")
    print(f"   - Unique users: {len(set(d['user_id'] for d in data))}")
    print(f"   - Action types: {set(d['action'] for d in data)}")
    print(f"   - Time range: {data[0]['timestamp']} ~ {data[-1]['timestamp']}")

if __name__ == '__main__':
    import os
    main()
