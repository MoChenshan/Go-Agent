#!/usr/bin/env python3
"""Generate Fibonacci numbers."""
import sys

def fib(n):
    a, b = 0, 1
    result = []
    for _ in range(n):
        result.append(a)
        a, b = b, a + b
    return result

if __name__ == "__main__":
    n = int(sys.argv[1]) if len(sys.argv) > 1 else 10
    numbers = fib(n)
    print(f"Fibonacci sequence (first {n} numbers):")
    print(", ".join(map(str, numbers)))
