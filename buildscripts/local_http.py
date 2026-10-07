"""Direct HTTP requests to disposable local test servers, with TLS validation."""
import urllib.request


def local_urlopen(request, *, timeout, context=None):
    # NO_PROXY in a child environment does not affect the Python parent or
    # proxies inherited from the operating system. Never proxy local evidence.
    opener = urllib.request.build_opener(
        urllib.request.ProxyHandler({}),
        urllib.request.HTTPSHandler(context=context),
    )
    return opener.open(request, timeout=timeout)
