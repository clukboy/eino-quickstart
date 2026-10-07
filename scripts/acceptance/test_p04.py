"""Regression for the live acceptance harness (no network and no live resources)."""
import argparse
import importlib.util
from pathlib import Path
import tempfile
import unittest

spec = importlib.util.spec_from_file_location('p04', Path(__file__).with_name('p04.py'))
p04 = importlib.util.module_from_spec(spec)
spec.loader.exec_module(p04)


class FakeAPI:
    def __init__(self):
        self.docs = {}
        self.ds = None
        self.next_id = 1
        self.calls = []
        self.bad_counts = False

    def request(self, method, path, data=None, expected=200, multipart=None):
        self.calls.append((method, path))
        if path == '/dataset':
            self.ds = {'id': 9, **data}
            return self.ds
        if path == '/dataset/9':
            if method == 'DELETE':
                self.ds = None
                return {'status': 'deleted'}
            return self.ds
        if path.endswith('/search'):
            if not data['query'].strip():
                return {'code': 'bad_request', 'error': 'query is required'}
            return {'data': [{'document_id': doc_id, 'content': doc['content']} for doc_id, doc in self.docs.items()
                             if data['query'] in doc['content']], 'channels': ['keyword'], 'degraded': [],
                    'top_k': 5, 'granularity': 'document', 'matched_chunks': 1, 'chunk_budget': 20}
        if path == '/dataset/9/reindex':
            return {'status': 'accepted', 'documents': len(self.docs), 'failed': 0, 'chunks': 0}
        if path.endswith('/upload'):
            data = {'content': '# multipart验收\n\n验收海豚词C，第二篇文档。'}
            path = '/dataset/9/documents'
        if path == '/dataset/9/documents':
            if method == 'GET':
                return {'data': list(self.docs.values())}
            if not data['content'].strip():
                return {'code': 'bad_request', 'error': 'content is required'}
            doc = {'id': self.next_id, 'status': 'ready', 'operation': 'created', 'content': data['content'],
                   'chunk_count': 1, 'indexed_chunk_count': 1}
            self.docs[self.next_id] = doc
            self.next_id += 1
            return {'data': [doc.copy()]}
        doc_id = int(path.split('/')[4])
        if doc_id not in self.docs:
            p04.require(expected == 404, 'missing document requires explicit 404 assertion')
            return {'code': 'not_found', 'error': 'document not found'}
        if method == 'DELETE':
            del self.docs[doc_id]
            return {'status': 'deleted'}
        if method == 'PUT':
            self.docs[doc_id].update(data)
        doc = self.docs[doc_id].copy()
        if self.bad_counts:
            doc['indexed_chunk_count'] = 0
        return doc


class HarnessTests(unittest.TestCase):
    def test_full_lifecycle_and_scoped_cleanup(self):
        with tempfile.TemporaryDirectory() as directory:
            client = FakeAPI()
            ledger = {'document_ids': [], 'deleted_document_ids': [], 'checkpoints': []}
            args = argparse.Namespace(timeout=1, expect_queue_outage=False, expect_paused_worker=False)
            p04.run(client, args, Path(directory), ledger)
            self.assertEqual(ledger['document_ids'], [1, 2])
            self.assertEqual(ledger['deleted_document_ids'], [1])
            self.assertIn(2, client.docs)  # preserved for user inspection by default
            p04.cleanup(client, ledger)
            self.assertEqual(client.docs, {})
            self.assertIsNone(client.ds)
            self.assertTrue(ledger['dataset_deleted'])

    def test_refuses_cleanup_identity_mismatch(self):
        client = FakeAPI()
        client.ds = {'id': 9, 'name': 'unrelated'}
        ledger = {'dataset_id': 9, 'dataset_name': 'acceptance-p04-test', 'document_ids': [1], 'deleted_document_ids': []}
        with self.assertRaises(p04.Failed):
            p04.cleanup(client, ledger)
        self.assertFalse(any(method == 'DELETE' for method, _ in client.calls))

    def test_refuses_deleting_untracked_documents(self):
        client = FakeAPI()
        client.ds = {'id': 9, 'name': 'acceptance-p04-test'}
        client.docs[999] = {'id': 999}
        ledger = {'dataset_id': 9, 'dataset_name': 'acceptance-p04-test', 'document_ids': [], 'deleted_document_ids': []}
        with self.assertRaises(p04.Failed):
            p04.cleanup(client, ledger)
        self.assertEqual(client.docs, {999: {'id': 999}})
        self.assertFalse(any(method == 'DELETE' for method, _ in client.calls))

    def test_ready_counters_are_not_a_false_pass(self):
        client = FakeAPI()
        client.docs[1] = {'id': 1, 'status': 'ready', 'chunk_count': 1, 'indexed_chunk_count': 0}
        with self.assertRaises(p04.Failed):
            p04.ready(client, '/dataset/9/documents/1', 1)

    def test_worker_failure_does_not_pass(self):
        client = FakeAPI()
        client.docs[1] = {'id': 1, 'status': 'failed'}
        with self.assertRaises(p04.Failed):
            p04.ready(client, '/dataset/9/documents/1', 1)

    def test_multipart_file_field_and_exact_body(self):
        body, content_type = p04.multipart_body('验收正文\n')
        self.assertIn('multipart/form-data; boundary=', content_type)
        self.assertIn(b'name="file"; filename="p04-upload.md"', body)
        self.assertIn('验收正文\n\r\n'.encode(), body)


if __name__ == '__main__':
    unittest.main()
