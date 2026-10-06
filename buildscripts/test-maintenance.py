#!/usr/bin/env python3
import importlib.util
import json
from pathlib import Path
import tempfile
import sys
import unittest
from unittest.mock import patch
from check_budgets import budgets, throughput_gate

spec = importlib.util.spec_from_file_location('sbom', Path(__file__).with_name('generate-sbom.py'))
sbom = importlib.util.module_from_spec(spec)
spec.loader.exec_module(sbom)

class MaintenanceTests(unittest.TestCase):
    def test_release_provenance(self):
        with tempfile.TemporaryDirectory() as temp:
            binary = Path(temp)/'oc'
            binary.write_bytes(b'fixture')
            clean = {'vcs':'git', 'vcs.modified':'false','vcs.revision':'abc123','vcs.time':'2026-10-07T00:00:00Z'}
            variants = [{k:v for k,v in clean.items() if k != 'vcs'}, {}, {'vcs.modified':'false'}, dict(clean, **{'vcs.modified':'true'}), clean]
            for settings in variants:
                info = {'GoVersion':'go1.27.1','Main':{'Version':'v1.0.0'},'Deps':[], 'Settings':[{'Key':k,'Value':v} for k,v in settings.items()] + [{'Key':'GO_EXTLINK_ENABLED'}]}
                with patch.object(sbom.subprocess,'check_output',return_value=json.dumps(info).encode()):
                    if settings == clean:
                        sbom.inventory(binary, True)
                    else:
                        with self.assertRaises(ValueError): sbom.inventory(binary, True)
            info['Deps']=[{'Path':'module','Replace':{'Path':'other','Version':'v1.0.0'}}]
            with patch.object(sbom.subprocess,'check_output',return_value=json.dumps(info).encode()):
                with self.assertRaises(ValueError): sbom.inventory(binary, True)

    def test_failed_scenario_retains_partial_metrics(self):
        spec = importlib.util.spec_from_file_location('integration', Path(__file__).with_name('test-core-integration.py'))
        integration = importlib.util.module_from_spec(spec)
        spec.loader.exec_module(integration)
        def fail(*args, record, **kwargs):
            record.update(scenario='test', checks=7, stability={'transferMetrics':[{'wallSeconds':1.5}]})
            raise ValueError('intentional failure')
        with tempfile.TemporaryDirectory() as temp:
            report = Path(temp)/'results.json'
            with patch.object(sys,'argv',['integration','--oc',__file__,'--otterio',__file__,'--stability-only','--report',str(report)]), patch.object(integration,'migration_checks',return_value={'scenario':'migration'}), patch.object(integration,'scenario',side_effect=fail):
                with self.assertRaises(ValueError): integration.main()
            failed = json.loads(report.read_text())['results'][1]
            self.assertEqual(failed['checks'],7)
            self.assertEqual(failed['stability']['transferMetrics'][0]['wallSeconds'],1.5)
            self.assertEqual(failed['status'],'failed')
            self.assertTrue(Path(str(report)+'.artifacts','failure-summary.json').is_file())

    def test_performance_gate(self):
        limits = budgets()
        floor = max(limits['minimumMiBPerSecond'],100*(1-limits['maximumThroughputDropFraction']))
        self.assertEqual(throughput_gate(100,100,limits),floor)
        for speed in (floor-1, 1):
            with self.assertRaises(AssertionError): throughput_gate(speed,100,limits)

if __name__ == '__main__': unittest.main()
