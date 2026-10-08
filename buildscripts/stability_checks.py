"""Disposable OC transfer baselines and slow-consumer signal checks."""
from concurrent.futures import ThreadPoolExecutor
import hashlib
import hmac
import datetime
import ssl
import urllib.request
import xml.etree.ElementTree as ET
import os
from pathlib import Path
import signal
import statistics
import subprocess
import sys
import threading
import time
import json
import urllib.parse
from fault_relay import FaultRelay
from check_budgets import budgets, paired_throughput_gate
from local_http import local_urlopen


class TransferProcess(subprocess.Popen):
    """Retain this child's exit RSS while communicate() drains both pipes.

    POSIX Popen's timed wait calls _try_wait under its waitpid lock. Use wait4
    there so a fast transfer cannot disappear before ps captures a sample.
    Call communicate before poll, which otherwise reaps through waitpid.
    """
    def __init__(self, *args, **kwargs):
        self.exit_peak_rss_kib = 0
        super().__init__(*args, **kwargs)

    def _try_wait(self, wait_flags):
        try:
            pid, status, usage = os.wait4(self.pid, wait_flags)
        except ChildProcessError:
            # Match Popen when another waiter has already reaped the child;
            # absent RSS accounting remains a measurement failure below.
            return self.pid, 0
        if pid == self.pid:
            # Darwin reports bytes; Linux reports KiB. This is per child,
            # unlike RUSAGE_CHILDREN's shared peak across concurrent transfers.
            self.exit_peak_rss_kib = usage.ru_maxrss / (1024 if sys.platform == 'darwin' else 1)
        return pid, status


def file_hash(path):
    with Path(path).open('rb') as stream:
        return hashlib.file_digest(stream, 'sha256').hexdigest()


def check_transfer_samples(metric, limits):
    """Gate fixed samples, retaining the real measured speed's absolute floor."""
    pairs = metric['samplePairs']
    for pair in pairs:
        pair['throughputRatio'] = pair['measured']['aggregateMiBPerSecond'] / pair['reference']['aggregateMiBPerSecond']
    measured = statistics.median(pair['measured']['aggregateMiBPerSecond'] for pair in pairs)
    reference = statistics.median(pair['reference']['aggregateMiBPerSecond'] for pair in pairs)
    paired_ratio = statistics.median(pair['throughputRatio'] for pair in pairs)
    metric.update(aggregateMiBPerSecond=measured, referenceMiBPerSecond=reference,
                  requiredMiBPerSecond=limits['minimumMiBPerSecond'],
                  pairedMedianRatio=paired_ratio,
                  requiredPairedRatio=1-limits['maximumThroughputDropFraction'])
    paired_throughput_gate(measured, paired_ratio, limits)


def transfer_performance_checks(transfer, run, source, root, expected, limits, evidence):
    """Compare three fixed fresh-target samples with single-transfer references."""
    size = source.stat().st_size
    checks = 0
    for concurrency in (1, 4):
        metrics = {}
        for operation in ('upload', 'download'):
            metric = {'operation': operation, 'concurrency': concurrency,
                      'objectBytes': size, 'transferredBytes': size * concurrency,
                      'warmup': {}, 'samplePairs': []}
            evidence['transferMetrics'].append(metric)
            metrics[operation] = metric
        # Reuse the same pool for both roles. Warmup primes its workers and the
        # CLI/server paths; every measured destination is still newly created.
        with ThreadPoolExecutor(max_workers=concurrency) as workers:
            for iteration in range(-1, 3):
                phase = 'warmup' if iteration == -1 else f'sample-{iteration}'
                order = ('reference', 'measured') if iteration == 1 else ('measured', 'reference')
                targets = {
                    role: [f'test/core-check/stability-{concurrency}-{phase}-{role}-{i}'
                           for i in range(concurrency if role == 'measured' else 1)]
                    for role in order
                }
                for operation in ('upload', 'download'):
                    pair = {'order': list(order)}
                    if iteration == -1:
                        metrics[operation]['warmup'] = pair
                    else:
                        metrics[operation]['samplePairs'].append(pair)
                    for role in order:
                        destinations = [root / (target.rsplit('/', 1)[-1] + '.download')
                                        for target in targets[role]]
                        commands = [(['cp', str(source), target] if operation == 'upload'
                                     else ['cp', target, str(destination)])
                                    for target, destination in zip(targets[role], destinations)]
                        samples = list(workers.map(transfer, commands))
                        elapsed = max(sample['finishedAt'] for sample in samples) - min(sample['startedAt'] for sample in samples)
                        if elapsed <= 0:
                            raise AssertionError('transfer timing must be positive')
                        pair[role] = {'transferredBytes': size * len(commands), 'wallSeconds': elapsed,
                                      'aggregateMiBPerSecond': size * len(commands) / elapsed / (1024 ** 2),
                                      'processSamples': samples}
                        checks += len(commands)
                        evidence['checks'] = checks
                        if operation == 'download':
                            for destination in destinations:
                                if file_hash(destination) != expected:
                                    raise AssertionError('concurrent transfer checksum mismatch')
                                destination.unlink()
                                checks += 1
                                evidence['checks'] = checks
                # Each upload has now been downloaded and hash-checked. Keep
                # the disposable fixture's disk use bounded between samples.
                run('rm', *(target for role in order for target in targets[role]))
        for metric in metrics.values():
            check_transfer_samples(metric, limits)
    return checks


def stability_checks(oc, config, env, root, run, files, soak_seconds=0, evidence=None, emit_console_error=None):
    limits = budgets()
    evidence = evidence if evidence is not None else {}
    evidence.update(checks=0, transferMetrics=[], transferAttempts=[], cancellation=[], interruptions=[], networkFaults=[], soak=None, budgets=limits)
    checks = 0
    source = files / 'large'
    expected = file_hash(source)
    size = source.stat().st_size

    def transfer(args):
        attempt = {"operation":args[0],"arguments":args[1:],"status":"running"}
        evidence["transferAttempts"].append(attempt)
        peak = 0
        sample_errors = []
        stopped = threading.Event()
        started = time.monotonic()
        child = TransferProcess([oc, '--config-dir', str(config), '--quiet', '--no-color', *args],
                                 env=env, stdout=subprocess.PIPE, stderr=subprocess.PIPE)
        def sample():
            nonlocal peak
            while not stopped.is_set():
                # ps reports resident KiB on Linux and macOS. This is a sampled
                # per-process peak, not total process-tree or server memory.
                try:
                    result = subprocess.run(['ps', '-o', 'rss=', '-p', str(child.pid)],
                                            capture_output=True, timeout=2)
                except (OSError, subprocess.TimeoutExpired) as error:
                    sample_errors.append(type(error).__name__)
                    return
                values = result.stdout.split()
                if values and values[0].isdigit():
                    peak = max(peak, int(values[0]))
                stopped.wait(0.1)
        sampler = threading.Thread(target=sample)
        sampler.start()
        finished = None
        try:
            child.communicate(timeout=limits["transferSeconds"])
            finished = time.monotonic()
            if child.returncode:
                raise RuntimeError(f'stability transfer returned {child.returncode}')
        finally:
            if child.poll() is None:
                child.kill()
                child.communicate()
            # Capture process-observed completion before waiting for ps/RSS
            # sampling to stop. Cleanup latency is evidence, not transfer time.
            if finished is None:
                finished = time.monotonic()
            stopped.set()
            sampler.join(timeout=3)
            process_peak = max(peak, child.exit_peak_rss_kib)
            attempt.update(exitCode=child.returncode, elapsedSeconds=round(finished-started,3),
                           samplerCleanupSeconds=round(time.monotonic()-finished,3),
                           exitPeakRSSMiB=round(child.exit_peak_rss_kib/1024,2),
                           processPeakRSSMiB=round(process_peak/1024,2),
                           sampledPeakRSSMiB=round(peak/1024,2), status="complete" if child.returncode==0 else "failed")
        if sample_errors or sampler.is_alive():
            raise AssertionError('RSS sampler failed or did not terminate')
        if process_peak == 0:
            raise AssertionError('transfer captured no process memory sample')
        if process_peak > limits["sampledProcessRSSMiB"] * 1024:
            raise AssertionError('transfer exceeded compatibility RSS budget')
        return {'startedAt': started, 'finishedAt': finished, 'elapsedSeconds': finished-started,
                'sampledPeakRSSMiB': round(peak / 1024, 2),
                'exitPeakRSSMiB': attempt['exitPeakRSSMiB'],
                'processPeakRSSMiB': attempt['processPeakRSSMiB'],
                'samplerCleanupSeconds': attempt['samplerCleanupSeconds']}

    checks = transfer_performance_checks(transfer, run, source, root, expected, limits, evidence)

    # Read the first event, then pause consumption until cancellation. Drain
    # pending output during graceful shutdown so blocked writes can return.
    cancellation = evidence["cancellation"]
    for command in (['watch', 'test/core-check'], ['admin', 'trace', 'test'],
                    ['admin', 'console', '--type', 'otterio', 'test']):
        for iteration in range(3):
            child = subprocess.Popen([oc, '--config-dir', str(config), '--json', '--no-color', *command],
                                     env=env, stdout=subprocess.PIPE, stderr=subprocess.PIPE)
            try:
                import selectors
                with selectors.DefaultSelector() as ready:
                    ready.register(child.stdout, selectors.EVENT_READ)
                    data = ""
                    deadline = time.monotonic() + 10
                    while time.monotonic() < deadline:
                        if command[0] == 'watch' or command[1] == 'trace':
                            run('cp', str(files / 'small'), 'test/core-check/subscription-ready')
                        else:
                            emit_console_error()
                        if ready.select(0.2):
                            line = os.read(child.stdout.fileno(), 65536)
                            data += line.decode(errors='replace')
                            try:
                                event_record, _ = json.JSONDecoder().raw_decode(data.lstrip())
                                if event_record.get('status') != 'success':
                                    raise AssertionError('subscription emitted an error instead of an event')
                                if command[0] == 'watch' and not event_record.get('events',{}).get('type'):
                                    raise AssertionError('watch readiness record contains no event')
                                break
                            except json.JSONDecodeError:
                                pass
                    else:
                        raise AssertionError('subscription produced no record before cancellation')
                if child.poll() is not None:
                    raise AssertionError('subscription exited before cancellation')
                if command[0] == 'watch' or command[1] == 'trace':
                    for event in range(24):
                        run('cp', str(files / 'small'), f'test/core-check/slow-consumer-{event}')
                started = time.monotonic()
                child.terminate()
                child.communicate(timeout=limits["cancellationSeconds"])
                elapsed = time.monotonic() - started
                cancellation.append({'command': ' '.join(command[:2]), 'iteration': iteration + 1,
                                     'exitCode': child.returncode, 'seconds': round(elapsed, 3), 'eventReceived':True})
                if child.returncode != 143:
                    raise AssertionError(f'unexpected cancellation status {child.returncode}')
                checks += 1
                evidence["checks"] = checks
            finally:
                if child.poll() is None:
                    child.kill()
                    child.wait()
                child.stdout.close()
                child.stderr.close()
    # Pause download consumption, then drain output during cancellation.
    # Subsequent requests must still work.
    interruptions = evidence["interruptions"]
    for command in (['cat', 'test/core-check/multipart'],):
        child = subprocess.Popen([oc, '--config-dir', str(config), '--no-color', *command],
                                 env=env, stdin=subprocess.PIPE if command[0] == 'pipe' else subprocess.DEVNULL,
                                 stdout=subprocess.PIPE, stderr=subprocess.PIPE)
        try:
            if command[0] == 'pipe':
                child.stdin.write(b'pending-upload' * 1024)
                child.stdin.flush()
            time.sleep(0.3)
            if child.poll() is not None:
                raise AssertionError('transfer completed before interruption')
            started = time.monotonic()
            child.terminate()
            child.communicate(timeout=limits["cancellationSeconds"])
            if child.returncode != 143:
                raise AssertionError('interrupted transfer reported success')
            interruptions.append({'command': command[0], 'exitCode': child.returncode,
                                  'seconds': round(time.monotonic() - started, 3)})
            checks += 1
            evidence["checks"] = checks
        finally:
            if child.poll() is None:
                child.kill()
                child.wait()
            if child.stdin:
                child.stdin.close()
            child.stdout.close()
            child.stderr.close()
    run('cp', str(files / 'small'), 'test/core-check/after-interruption')
    if run('cat', 'test/core-check/after-interruption') != (files / 'small').read_bytes():
        raise AssertionError('server did not recover after interrupted transfers')
    checks += 1
    evidence["checks"] = checks
    # Preserve the TLS stream and AWS signatures; throttle/drop TCP bytes only.
    configured = json.loads((config / 'config.json').read_text())['aliases']['test']
    endpoint = urllib.parse.urlsplit(configured['url'])

    def multipart_state(key):
        def query(path, values):
            canonical_query = '&'.join(urllib.parse.quote(k, safe='')+'='+urllib.parse.quote(v, safe='') for k,v in sorted(values.items()))
            stamp = datetime.datetime.now(datetime.timezone.utc).strftime('%Y%m%dT%H%M%SZ')
            day = stamp[:8]
            body_hash = hashlib.sha256(b'').hexdigest()
            headers = {'host':endpoint.netloc, 'x-amz-content-sha256':body_hash, 'x-amz-date':stamp}
            signed = ';'.join(sorted(headers))
            canonical = '\n'.join(['GET',path,canonical_query,''.join(k+':'+headers[k]+'\n' for k in sorted(headers)),signed,body_hash])
            scope = day+'/us-east-1/s3/aws4_request'
            message = '\n'.join(['AWS4-HMAC-SHA256',stamp,scope,hashlib.sha256(canonical.encode()).hexdigest()])
            signing = ('AWS4'+env['OTTERIO_ROOT_PASSWORD']).encode()
            for part in [day,'us-east-1','s3','aws4_request']:
                signing = hmac.new(signing,part.encode(),hashlib.sha256).digest()
            headers['Authorization'] = 'AWS4-HMAC-SHA256 Credential='+env['OTTERIO_ROOT_USER']+'/'+scope+', SignedHeaders='+signed+', Signature='+hmac.new(signing,message.encode(),hashlib.sha256).hexdigest()
            request = urllib.request.Request(configured['url']+path+'?'+canonical_query,headers=headers)
            context = ssl.create_default_context(cafile=str(config/'certs/CAs/s3.crt')) if endpoint.scheme=='https' else None
            with local_urlopen(request,context=context,timeout=5) as response:
                tree = ET.fromstring(response.read())
            for item in tree.iter(): item.tag = item.tag.split('}')[-1]
            return tree
        sessions = query('/core-check', {'uploads':'','prefix':key})
        uploads = [item for item in sessions.findall('Upload') if item.findtext('Key')==key]
        total = 0
        for upload in uploads:
            parts = query('/core-check/'+key, {'uploadId':upload.findtext('UploadId')})
            total += sum(int(part.findtext('Size')) for part in parts.findall('Part'))
        return len(uploads), total

    faults = evidence["networkFaults"]
    with FaultRelay(endpoint.hostname, endpoint.port) as relay:
        proxied = urllib.parse.urlunsplit((endpoint.scheme, '127.0.0.1:' + str(relay.port), '', '', ''))
        run('alias', 'set', 'fault', proxied, env['OTTERIO_ROOT_USER'], env['OTTERIO_ROOT_PASSWORD'],
            '--api', 's3v4', '--path', 'on')
        relay.mode = 'slow'
        slow_destination = root / 'slow.download'
        started = time.monotonic()
        run('cp', 'fault/core-check/multipart', str(slow_destination), command_timeout=limits["transferSeconds"])
        if file_hash(slow_destination) != expected or relay.throttled_bytes < size:
            raise AssertionError('throttled download was not complete or verified')
        faults.append({'fault': '8-MiB/s-TCP-throttle', 'seconds': round(time.monotonic()-started, 3), 'hashVerified': True})
        checks += 1
        evidence["checks"] = checks
        relay.mode = 'drop'
        broken_destination = root / 'interrupted.download'
        result = subprocess.run([oc, '--config-dir', str(config), '--no-color', 'cp',
                                 'fault/core-check/multipart', str(broken_destination)],
                                env=env, capture_output=True, timeout=limits["transferSeconds"])
        if not relay.dropped:
            raise AssertionError('relay failed to inject a disconnect')
        if result.returncode == 0 and file_hash(broken_destination) != expected:
            raise AssertionError('truncated download reported success')
        # Retry explicitly when the command reports failure; never treat a
        # disconnected transfer as successful merely because a file exists.
        if result.returncode:
            run('cp', 'fault/core-check/multipart', str(broken_destination), command_timeout=limits["transferSeconds"])
        if file_hash(broken_destination) != expected:
            raise AssertionError('download did not recover after connection drop')
        faults.append({'fault': 'one-TCP-disconnect-after-1-MiB', 'firstExitCode': result.returncode, 'hashVerified': True})
        checks += 1
        evidence["checks"] = checks
    with FaultRelay(endpoint.hostname, endpoint.port) as upload_relay:
        url = urllib.parse.urlunsplit((endpoint.scheme, '127.0.0.1:'+str(upload_relay.port), '', '', ''))
        run('alias', 'set', 'uploadfault', url, env['OTTERIO_ROOT_USER'], env['OTTERIO_ROOT_PASSWORD'], '--api', 's3v4', '--path', 'on')
        upload_relay.mode = 'upload-slow'
        child = subprocess.Popen([oc, '--config-dir', str(config), '--quiet', 'cp', '--continue', str(source),
                                  'uploadfault/core-check/multipart-abort'], env=env, stdout=subprocess.PIPE, stderr=subprocess.PIPE)
        try:
            # A complete part must traverse the throttled TLS relay before
            # ListParts can observe it. Use the transfer budget, not a shorter
            # wall-clock assumption; keep the cancellation budget unchanged.
            deadline = time.monotonic()+limits['transferSeconds']
            while time.monotonic() < deadline:
                sessions, uploaded_bytes = multipart_state('multipart-abort')
                evidence['activeUpload'] = {'sessions':sessions,'uploadedPartBytes':uploaded_bytes}
                if sessions and uploaded_bytes > 0:
                    break
                if child.poll() is not None:
                    raise AssertionError('upload finished before multipart observation: '+str(child.returncode)+' '+child.communicate()[1].decode(errors='replace'))
                time.sleep(0.1)
            else:
                raise AssertionError('no uploaded multipart part observed within transfer budget: '+str(evidence['activeUpload']))
            child.terminate()
            child.communicate(timeout=limits['cancellationSeconds'])
            if child.returncode != 143:
                raise AssertionError('multipart cancellation did not unwind gracefully')
            remaining, _ = multipart_state('multipart-abort')
            interruptions.append({'command':'multipart-upload', 'sessionObserved':True, 'uploadedPartBytes':uploaded_bytes, 'residualSessions':remaining, 'exitCode':child.returncode})
            if remaining:
                raise AssertionError('canceled multipart upload leaked its session')
            checks += 1
            evidence["checks"] = checks
        finally:
            if child.poll() is None:
                child.kill(); child.communicate()
            child.stdout.close(); child.stderr.close()
        upload_relay.mode = 'upload-drop'
        upload_relay.dropped = False
        result = subprocess.run([oc, '--config-dir', str(config), '--quiet', 'cp', str(source), 'uploadfault/core-check/network-upload'], env=env, capture_output=True, timeout=limits['transferSeconds'])
        if not upload_relay.dropped:
            raise AssertionError('upload disconnect not injected')
        if result.returncode:
            run('cp', str(source), 'uploadfault/core-check/network-upload', command_timeout=limits['transferSeconds'])
        destination = root / 'network-upload.download'
        run('cp', 'test/core-check/network-upload', str(destination))
        if file_hash(destination) != expected:
            raise AssertionError('upload recovery hash mismatch')
        remaining, _ = multipart_state('network-upload')
        if remaining:
            raise AssertionError('upload disconnect leaked multipart session')
        faults.append({'fault':'upload-TCP-disconnect', 'firstExitCode':result.returncode,'hashVerified':True,'residualSessions':0})
        checks += 1
        evidence["checks"] = checks
    soak = None
    if soak_seconds:
        child = subprocess.Popen([oc, '--config-dir', str(config), '--json', '--no-color',
                                  'watch', 'test/core-check'], env=env,
                                 stdout=subprocess.PIPE, stderr=subprocess.PIPE)
        started = time.monotonic()
        rss_samples = []
        evidence["soak"] = {"status":"running", "rssSamplesMiB":rss_samples}
        try:
            while time.monotonic() - started < soak_seconds:
                if child.poll() is not None:
                    raise AssertionError('soak subscription exited unexpectedly')
                run('cp', str(files / 'small'), 'test/core-check/soak-event')
                sample = subprocess.run(['ps', '-o', 'rss=', '-p', str(child.pid)],
                                        capture_output=True, timeout=2).stdout.split()
                if sample and sample[0].isdigit():
                    rss_samples.append(int(sample[0]) / 1024)
                time.sleep(0.25)
            if not rss_samples or max(rss_samples) > limits["sampledProcessRSSMiB"] or max(rss_samples)-rss_samples[0] > limits["soakGrowthMiB"]:
                raise AssertionError('soak RSS budget exceeded or no samples available')
            soak = {'seconds': round(time.monotonic()-started, 2), 'samples': len(rss_samples),
                    'initialRSSMiB': round(rss_samples[0], 2), 'peakRSSMiB': round(max(rss_samples), 2),
                    'maxGrowthBudgetMiB': limits['soakGrowthMiB'], 'stdoutConsumed': False}
            child.terminate()
            child.communicate(timeout=limits["cancellationSeconds"])
            if child.returncode != 143:
                raise AssertionError('soak cancellation reported an unexpected status')
            checks += 1
            evidence["checks"] = checks
        finally:
            if child.poll() is None:
                child.kill()
                child.wait()
            child.stdout.close()
            child.stderr.close()
    evidence.update(checks=checks, soak=soak)
    evidence['methodology'] = ('65 MiB objects; concurrency 1 and 4 against single-transfer references; '
                               'one paired warmup and three fixed alternating sample pairs per operation; '
                               'fresh remote keys and local files; every download hash-checked; '
                               'median paired ratios plus the real measured throughput median floor; local disposable server; '
                               'process-observed wall time includes CLI startup, excludes sampler cleanup; '
                               'OC RSS sampled every 100 ms; budgets read from compatibility.json; '
                               'subscriptions receive an event before pausing output; cancellation drains pending output; no disconnected-event replay claim')
    return evidence
