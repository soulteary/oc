"""ZIP bytes may arrive before the server releases its archive slot."""
import json
import unittest
from unittest.mock import patch

from console_features_acceptance import wait_archive_download


class ArchiveDownloadCompletionTests(unittest.TestCase):
    def reader(self, statuses):
        calls = []
        states = iter(statuses)

        def request(path, method='GET', *args, **kwargs):
            self.assertEqual(method, 'GET', 'completion must never repeat a mutation')
            self.assertEqual(path, '/api/archives/archive-id')
            calls.append(path)
            return json.dumps({'id': 'archive-id', 'status': next(states)}).encode(), {}

        return request, calls

    @patch('console_features_acceptance.time.sleep')
    def test_received_zip_waits_through_pending_download_cleanup(self, sleep):
        request, calls = self.reader(['downloading', 'downloading', 'succeeded'])
        result = wait_archive_download(request, {'id': 'archive-id', 'status': 'ready'})
        self.assertEqual(result['status'], 'succeeded')
        self.assertEqual(len(calls), 3)
        self.assertEqual(sleep.call_count, 2)

    @patch('console_features_acceptance.time.sleep')
    def test_completed_download_needs_no_delay(self, sleep):
        request, calls = self.reader(['succeeded'])
        wait_archive_download(request, {'id': 'archive-id'})
        self.assertEqual(len(calls), 1)
        sleep.assert_not_called()

    @patch('console_features_acceptance.time.sleep')
    def test_failed_or_canceled_download_is_not_accepted(self, sleep):
        for status in ('failed', 'canceled', 'ready'):
            with self.subTest(status=status):
                request, calls = self.reader(['downloading', status])
                with self.assertRaises(AssertionError):
                    wait_archive_download(request, {'id': 'archive-id'})
                self.assertEqual(len(calls), 2)

    @patch('console_features_acceptance.time.sleep')
    @patch('console_features_acceptance.time.monotonic', side_effect=[0, 21])
    def test_stuck_download_fails_within_existing_budget(self, clock, sleep):
        request, calls = self.reader(['downloading'])
        with self.assertRaisesRegex(AssertionError, 'fixture budget'):
            wait_archive_download(request, {'id': 'archive-id'})
        self.assertEqual(len(calls), 1)
        sleep.assert_not_called()


if __name__ == '__main__':
    unittest.main()
