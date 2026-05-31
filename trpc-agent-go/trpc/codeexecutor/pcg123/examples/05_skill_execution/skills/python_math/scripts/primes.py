#!/usr/bin/env python3
"""Generate prime numbers up to N."""
import sys

def sieve(n):
    is_prime = [True] * (n + 1)
    is_prime[0] = is_prime[1] = False
    for i in range(2, int(n**0.5) + 1):
        if is_prime[i]:
            for j in range(i*i, n + 1, i):
                is_prime[j] = False
    return [i for i in range(n + 1) if is_prime[i]]

if __name__ == "__main__":
    n = int(sys.argv[1]) if len(sys.argv) > 1 else 100
    primes = sieve(n)
    print(f"Prime numbers up to {n}:")
    print(", ".join(map(str, primes)))
