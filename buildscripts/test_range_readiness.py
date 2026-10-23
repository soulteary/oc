"""Write readiness must precede Range fixture mutations, without write retries."""
import importlib.util
import io
from email.message import Message
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from pathlib import Path
import threading
import unittest
from unittest.mock import Mock, patch
import urllib.error
import urllib.request

spec = importlib.util.spec_from_file_location('fixture', Path(__file__).with_name('test-console-integration.py'))
fixture = importlib.util.module_from_spec(spec)
spec.loader.exec_module(fixture)


class Clock:
    def __init__(self): self.now = 0
    def monotonic(self): return self.now
    def sleep(self, duration): self.now += duration


def reply(offline=False):
    result = Mock()
    result.__enter__ = Mock(return_value=result)
    result.__exit__ = Mock(return_value=False)
    result.status = 200
    result.headers = Message()
    if offline: result.headers['X-OtterIO-Server-Status'] = 'offline'
    return result


class ReadinessTests(unittest.TestCase):
    def run_wait(self, responses, timeout=0.3, process=None):
        clock = Clock()
        with patch.object(fixture, 'local_urlopen', side_effect=responses) as request, \
             patch.object(fixture.time, 'monotonic', clock.monotonic), \
             patch.object(fixture.time, 'sleep', clock.sleep):
            fixture.wait_for_writable_server(process or Mock(poll=Mock(return_value=None)),
                                             'https://127.0.0.1:12345', context='independent-ca', timeout=timeout)
        return request

    def test_503_and_200_offline_do_not_allow_fixture_writes(self):
        unavailable = urllib.error.HTTPError('https://fixture', 503, '', {}, io.BytesIO())
        request = self.run_wait([unavailable, reply(offline=True), reply()])
        self.assertEqual(request.call_count, 3)
        for call in request.call_args_list:
            self.assertEqual(call.args, ('https://127.0.0.1:12345/otterio/health/cluster',))
            self.assertEqual(call.kwargs['context'], 'independent-ca')
            self.assertGreater(call.kwargs['timeout'], 0)
            self.assertLessEqual(call.kwargs['timeout'], 0.3)

    def test_exit_fails_before_any_request(self):
        with self.assertRaisesRegex(RuntimeError, 'server exited'):
            self.run_wait([], process=Mock(poll=Mock(return_value=1)))

    def test_offline_and_connection_failure_obey_one_deadline(self):
        for response in (reply(offline=True), urllib.error.URLError('not listening')):
            with self.subTest(response=type(response).__name__), \
                 self.assertRaisesRegex(RuntimeError, 'before deadline'):
                self.run_wait(lambda *args, **kwargs: response if not isinstance(response, Exception)
                              else (_ for _ in ()).throw(response))

    def test_auth_failure_and_unexpected_errors_are_not_retried(self):
        for error in (urllib.error.HTTPError('https://fixture', 403, '', {}, io.BytesIO()), ValueError('invalid probe')):
            with self.subTest(error=type(error).__name__), self.assertRaises((RuntimeError, ValueError)):
                self.run_wait([error])

    def test_response_after_deadline_cannot_succeed(self):
        clock = Clock()
        def late_response(*args, **kwargs):
            clock.now = 0.4
            return reply()
        with patch.object(fixture, 'local_urlopen', side_effect=late_response) as request, \
             patch.object(fixture.time, 'monotonic', clock.monotonic), \
             patch.object(fixture.time, 'sleep', clock.sleep), \
             self.assertRaisesRegex(RuntimeError, 'before deadline'):
            fixture.wait_for_writable_server(Mock(poll=Mock(return_value=None)),
                                             'http://127.0.0.1:12345', timeout=0.3)
        self.assertEqual(request.call_count, 1)

    def test_liveness_200_can_fail_a_write_while_cluster_probe_waits(self):
        ready = threading.Event()
        writes = []
        class Handler(BaseHTTPRequestHandler):
            def log_message(self, *args): pass
            def do_GET(self):
                live = self.path.endswith('/ready')
                self.send_response(200 if live or ready.is_set() else 503)
                if not ready.is_set(): self.send_header('X-OtterIO-Server-Status', 'offline')
                if ready.is_set(): self.send_header('X-OtterIO-Write-Quorum', '3')
                self.end_headers()
            def do_PUT(self):
                writes.append(ready.is_set())
                self.send_response(200 if ready.is_set() else 503)
                self.end_headers()
        server = ThreadingHTTPServer(('127.0.0.1', 0), Handler)
        thread = threading.Thread(target=server.serve_forever, daemon=True)
        thread.start()
        endpoint = 'http://127.0.0.1:' + str(server.server_port)
        try:
            with fixture.local_urlopen(endpoint + '/otterio/health/ready', timeout=1) as live:
                self.assertEqual(live.status, 200)
                self.assertEqual(live.headers['X-OtterIO-Server-Status'], 'offline')
            with self.assertRaises(urllib.error.HTTPError) as failed_write:
                fixture.local_urlopen(urllib.request.Request(endpoint + '/range-fixture', method='PUT'), timeout=1)
            failed_write.exception.close()
            timer = threading.Timer(0.1, ready.set)
            timer.start()
            fixture.wait_for_writable_server(Mock(poll=Mock(return_value=None)), endpoint, timeout=2)
            timer.join()
            self.assertEqual(writes, [False], 'readiness must not retry the fixture write')
            with fixture.local_urlopen(urllib.request.Request(endpoint + '/range-fixture', method='PUT'), timeout=1) as created:
                self.assertEqual(created.status, 200)
            self.assertEqual(writes, [False, True])
        finally:
            server.shutdown()
            server.server_close()
            thread.join()


if __name__ == '__main__': unittest.main()
