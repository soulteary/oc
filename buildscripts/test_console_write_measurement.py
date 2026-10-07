"""Fixed paired timing gates must tolerate noise without hiding regressions."""
import hashlib
import math
import statistics
import unittest

import console_write_acceptance as acceptance


def trials(cli_seconds, console_seconds):
    return [{'cliUploadSeconds': cli, 'consoleUploadSeconds': console}
            for cli, console in zip(cli_seconds, console_seconds)]


class ConsoleWriteMeasurementTests(unittest.TestCase):
    def test_exact_existing_rate_and_ratio_budgets_pass(self):
        summary = acceptance.summarize_upload_trials(trials([6.5] * 5, [13.0] * 5), 65)
        self.assertEqual(summary['consoleMiBPerSecond'], 5)
        self.assertEqual(summary['medianConsoleToCLIRatio'], 0.5)
        acceptance.require_upload_budget(summary)

    def test_below_either_budget_fails_without_rounding_or_epsilon(self):
        for cli, console in [(1, 2.000000001), (6.6, 13.000000001)]:
            with self.subTest(cli=cli, console=console):
                summary = acceptance.summarize_upload_trials(trials([cli] * 5, [console] * 5), 65)
                with self.assertRaises(AssertionError):
                    acceptance.require_upload_budget(summary)

    def test_adjacent_pairs_preserve_common_host_load_changes(self):
        cli = [0.1, 0.1, 0.2, 1, 1]
        console = [0.19, 0.19, 2, 1.9, 1.9]
        self.assertLess(statistics.median(cli) / statistics.median(console), 0.5)
        summary = acceptance.summarize_upload_trials(trials(cli, console), 65)
        self.assertEqual(summary['pairedConsoleToCLIRatios'], [c / b for c, b in zip(cli, console)])
        self.assertGreater(summary['medianConsoleToCLIRatio'], 0.5)
        acceptance.require_upload_budget(summary)

    def test_two_fast_outliers_do_not_hide_three_failing_pairs(self):
        cli = [0.1, 0.1, 0.2, 1, 1]
        console = [0.25, 0.25, 0.5, 0.1, 0.1]
        self.assertGreater(statistics.median(cli) / statistics.median(console), 0.5)
        summary = acceptance.summarize_upload_trials(trials(cli, console), 65)
        self.assertEqual(summary['medianConsoleToCLIRatio'], 0.4)
        self.assertEqual(len(summary['pairedConsoleToCLIRatios']), 5)
        with self.assertRaises(AssertionError):
            acceptance.require_upload_budget(summary)

    def test_first_ci_failure_is_not_reclassified_as_a_pass(self):
        cli = [0.1833773089999795, 0.14884802200003833, 0.1492243730000382]
        console = [0.2967882720000148, 0.2991010929999902, 0.32158024999995405]
        summary = acceptance.summarize_upload_trials(trials(cli + cli[1:], console + console[1:]), 65)
        self.assertLess(summary['medianConsoleToCLIRatio'], 0.5)
        with self.assertRaises(AssertionError):
            acceptance.require_upload_budget(summary)

    def test_incomplete_or_extended_samples_cannot_change_the_fixed_gate(self):
        for count in (0, 3, 4, 6):
            with self.subTest(count=count), self.assertRaises(ValueError):
                acceptance.summarize_upload_trials(trials([1] * count, [2] * count), 65)

    def test_invalid_times_cannot_contribute_to_a_successful_summary(self):
        for invalid in (0, -1, math.inf, -math.inf, math.nan):
            for path in ('cliUploadSeconds', 'consoleUploadSeconds'):
                sample = trials([1] * 5, [2] * 5)
                sample[2][path] = invalid
                with self.subTest(path=path, invalid=invalid), self.assertRaises(ValueError):
                    acceptance.summarize_upload_trials(sample, 65)

    def test_hash_and_size_are_verified_for_each_transfer(self):
        payload = b'paired upload verification'
        expected = hashlib.sha256(payload).hexdigest()
        self.assertEqual(acceptance.verify_transfer_payload(payload, len(payload), expected), expected)
        for changed in (payload[:-1], payload + b'x', payload[:-1] + b'x'):
            with self.subTest(payload=changed), self.assertRaises(AssertionError):
                acceptance.verify_transfer_payload(changed, len(payload), expected)


if __name__ == '__main__':
    unittest.main()
