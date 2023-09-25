"""
This code is adapted from FunctionBench:
https://github.com/ddps-lab/serverless-faas-workbench/blob/cf3e1e9c14870788a38dc38c5bb4be9e18fdcf3c/openwhisk/cpu-memory/matmul/function.py

The original code is licensed under the Apache License 2.0
"""

import numpy as np
from time import time


def matmul(n):
    A = np.random.rand(n, n)
    B = np.random.rand(n, n)

    start = time()
    C = np.matmul(A, B)
    latency = time() - start
    return latency


def main(event):
    latencies = {}
    timestamps = {}
    
    timestamps["starting_time"] = time()
    n = int(event['n'])
    metadata = event['metadata']
    result = matmul(n)
    latencies["function_execution"] = result
    timestamps["finishing_time"] = time()

    return {"latencies": latencies, "timestamps": timestamps, "metadata": metadata}