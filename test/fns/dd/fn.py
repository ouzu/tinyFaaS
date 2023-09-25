"""
This code is adapted from FunctionBench:
https://github.com/ddps-lab/serverless-faas-workbench/blob/cf3e1e9c14870788a38dc38c5bb4be9e18fdcf3c/google/disk/dd/main.py

The original code is licensed under the Apache License 2.0
"""

import subprocess

def main(event):
    bs = 'bs='+event['bs']
    count = 'count='+event['count']
    print(bs)
    print(count)
    out_fd = open('/tmp/io_write_logs','w')
    a = subprocess.Popen(['dd', 'if=/dev/zero', 'of=/tmp/out', bs, count], stderr=out_fd)
    a.communicate()
    
    output = subprocess.check_output(['ls', '-alh', '/tmp/'])
    print(output)

    output = subprocess.check_output(['du', '-sh', '/tmp/'])
    print(output)
                               
    with open('/tmp/io_write_logs') as logs:
        result = str(logs.readlines()[2]).replace('\n', '')
        print(result)
        return result