#!/usr/bin/env python3
# Copyright (c) 2026 MiniStack Contributors. SPDX-License-Identifier: MIT
# Copies or substantial portions must retain this notice (see MiniStack LICENSE).
"""Install the CodeBuild DeleteProject compatibility fix for MiniStack 1.5.43."""

import argparse
from pathlib import Path

ORIGINAL = '''def _delete_project(data):
    name = data.get("name", "")
    if not name or name not in _projects:
        return error_response_json("ResourceNotFoundException",
                                   f"Project not found: {name}", 400)
    del _projects[name]
    logger.info("DeleteProject: %s", name)
    return json_response({})
'''

PATCHED = '''def _delete_project(data):
    identifier = data.get("name", "")
    name = _project_name_from_identifier(identifier)
    if not name or name not in _projects:
        return error_response_json("ResourceNotFoundException",
                                   f"Project not found: {identifier}", 400)
    del _projects[name]
    logger.info("DeleteProject: %s", name)
    return json_response({})
'''


def patch(target: Path) -> None:
    source = target.read_text(encoding="utf-8")
    if source.count(PATCHED) == 1 and ORIGINAL not in source:
        return
    if source.count(ORIGINAL) != 1 or PATCHED in source:
        raise ValueError(f"unsupported MiniStack 1.5.43 CodeBuild layout: {target}")
    target.write_text(source.replace(ORIGINAL, PATCHED, 1), encoding="utf-8")


def main() -> None:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument(
        "target",
        type=Path,
        nargs="?",
        default=Path("/opt/ministack/ministack/services/codebuild.py"),
    )
    args = parser.parse_args()
    try:
        patch(args.target)
    except ValueError as error:
        parser.error(str(error))


if __name__ == "__main__":
    main()
