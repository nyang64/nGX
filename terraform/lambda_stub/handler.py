"""
Stub Lambda handler — placeholder until real Go binaries are built.

Replace by running: make build-lambdas
Each function will be deployed from builds/<name>.zip containing a
compiled Go binary named 'bootstrap' for the provided.al2023 runtime.
"""
import json


def handler(event, context):
    return {
        "statusCode": 501,
        "headers": {"Content-Type": "application/json"},
        "body": json.dumps({"error": "not implemented — stub handler"}),
    }
