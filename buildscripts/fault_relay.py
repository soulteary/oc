"""Bounded local TCP relay for TLS-preserving transfer fault injection."""
import socket
import threading
import time


class FaultRelay:
    def __init__(self, host, port):
        self.upstream = (host, port)
        self.listener = socket.socket()
        self.listener.bind(('127.0.0.1', 0))
        self.listener.listen()
        self.listener.settimeout(0.2)
        self.port = self.listener.getsockname()[1]
        self.stopped = threading.Event()
        self.connections = []
        self.workers = []
        self.lock = threading.Lock()
        self.mode = 'normal'
        self.dropped = False
        self.throttled_bytes = 0
        self.thread = threading.Thread(target=self.accept)

    def __enter__(self):
        self.thread.start()
        return self

    def accept(self):
        while not self.stopped.is_set():
            try:
                client, _ = self.listener.accept()
            except socket.timeout:
                continue
            except OSError:
                break
            worker = threading.Thread(target=self.bridge, args=(client,))
            self.workers.append(worker)
            worker.start()

    def bridge(self, client):
        try:
            upstream = socket.create_connection(self.upstream, timeout=3)
            upstream.settimeout(None)
        except OSError:
            client.close()
            return
        with self.lock:
            self.connections.extend((client, upstream))
        def pump(source, target, downstream):
            transferred = 0
            try:
                while not self.stopped.is_set():
                    chunk = source.recv(64 * 1024)
                    if not chunk:
                        break
                    if downstream and self.mode == 'drop' and transferred >= 1024 * 1024:
                        with self.lock:
                            should_drop = not self.dropped
                            self.dropped = True
                        if should_drop:
                            break
                    if not downstream and self.mode == 'upload-slow':
                        if self.stopped.wait(len(chunk)/(2*1024*1024)):
                            break
                    if not downstream and self.mode == 'upload-drop' and transferred >= 1024*1024:
                        with self.lock:
                            should_drop = not self.dropped
                            self.dropped = True
                        if should_drop:
                            break
                    if downstream and self.mode == 'slow':
                        with self.lock:
                            self.throttled_bytes += len(chunk)
                        if self.stopped.wait(len(chunk) / (8 * 1024 * 1024)):
                            break
                    target.sendall(chunk)
                    transferred += len(chunk)
            except OSError:
                pass
            finally:
                for connection in (source, target):
                    try:
                        connection.shutdown(socket.SHUT_RDWR)
                    except OSError:
                        pass
        upload = threading.Thread(target=pump, args=(client, upstream, False))
        upload.start()
        pump(upstream, client, True)
        upload.join(timeout=4)
        for connection in (client, upstream):
            connection.close()
        with self.lock:
            self.connections.remove(client)
            self.connections.remove(upstream)

    def __exit__(self, *_):
        self.stopped.set()
        self.listener.close()
        with self.lock:
            for connection in self.connections:
                try:
                    connection.shutdown(socket.SHUT_RDWR)
                except OSError:
                    pass
        self.thread.join(timeout=4)
        for worker in self.workers:
            worker.join(timeout=4)
        if self.thread.is_alive() or any(worker.is_alive() for worker in self.workers):
            raise AssertionError('fault relay failed to release workers')
