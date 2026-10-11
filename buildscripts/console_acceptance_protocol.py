"""Shared checks for evidence from disposable Console acceptance runs."""
import xml.etree.ElementTree as ET


def legacy_route_disabled(status, body):
    """Authentication errors from a legacy handler do not prove removal."""
    if status == 404:
        return True
    if status not in (400, 403):
        return False
    try:
        error = ET.fromstring(body)
    except ET.ParseError:
        return False
    return error.tag.rsplit('}', 1)[-1] == 'Error' and bool((error.findtext('{*}Code') or '').strip())
