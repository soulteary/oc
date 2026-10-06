"""Shared, validated compatibility budgets and performance gates."""
import json
from pathlib import Path


def budgets():
    values = json.loads((Path(__file__).resolve().parents[1] / 'docs/compatibility.json').read_text())['stabilityBudgets']
    required = ('transferSeconds', 'sampledProcessRSSMiB', 'cancellationSeconds', 'soakGrowthMiB', 'minimumMiBPerSecond', 'maximumThroughputDropFraction')
    if any(key not in values for key in required):
        raise ValueError('incomplete stability budgets')
    if any(values[key] <= 0 for key in required[:-1]) or not 0 < values[required[-1]] < 1:
        raise ValueError('invalid stability budgets')
    return values


def throughput_gate(actual, baseline, limits):
    floor = max(limits['minimumMiBPerSecond'], baseline * (1-limits['maximumThroughputDropFraction']))
    if actual < floor:
        raise AssertionError(f'throughput {actual:.2f} MiB/s below {floor:.2f} MiB/s gate')
    return floor
