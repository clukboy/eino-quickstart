#!/usr/bin/env python3
"""Live HTTP acceptance only; never starts services or reads DB/model secrets."""
import argparse
import datetime
import json
import os
from pathlib import Path
import sys
import time
import urllib.error
import urllib.parse
import urllib.request
import uuid


class Blocked(Exception):
    pass


class Failed(Exception):
    pass


def require(condition, message):
    if not condition:
        raise Failed(message)


class NoRedirect(urllib.request.HTTPRedirectHandler):
    def redirect_request(self, req, fp, code, msg, headers, newurl):
        return None  # Do not forward a bearer credential to another endpoint.


class Client:
    def __init__(self, base, token, report):
        self.base = base.rstrip('/')
        self.token = token
        self.report = report
        self.sequence = 0
        self.opener = urllib.request.build_opener(NoRedirect)

    def request(self, method, path, data=None, expected=200, multipart=None):
        headers = {'Authorization': 'Bearer ' + self.token}
        body = None
        if multipart is not None:
            body, headers['Content-Type'] = multipart
        elif data is not None:
            body = json.dumps(data, ensure_ascii=False).encode()
            headers['Content-Type'] = 'application/json'
        request = urllib.request.Request(self.base + path, body, headers, method=method)
        self.sequence += 1
        started = time.monotonic()
        try:
            response = self.opener.open(request, timeout=10)
        except urllib.error.HTTPError as exc:
            response = exc
        except (urllib.error.URLError, OSError) as exc:
            raise Blocked(f'API is unreachable ({type(exc).__name__}); verify isolated API is running') from None
        with response:
            status = response.code
            raw = response.read()
        try:
            result = json.loads(raw)
        except (UnicodeDecodeError, json.JSONDecodeError):
            raise Failed(f'{method} {path}: HTTP {status}, expected JSON') from None
        # Persist paths, safe test payloads and bodies, NEVER request headers/tokens.
        record = {'method': method, 'path': path, 'request': data,
                  'status': status, 'response': result,
                  'elapsed_seconds': round(time.monotonic() - started, 3)}
        save_json(self.report / f'{self.sequence:04d}-http.json', record)
        if status in (401, 403):
            raise Blocked('API refused the acceptance credential; use an authorized test administrator')
        require(status in expected if isinstance(expected, tuple) else status == expected, f'{method} {path}: HTTP {status}, expected {expected}; see HTTP report')
        return result


def save_json(path, data):
    path.write_text(json.dumps(data, ensure_ascii=False, indent=2) + '\n', encoding='utf-8')


def ready(client, path, timeout):
    deadline = time.monotonic() + timeout
    while True:
        doc = client.request('GET', path)
        require(doc.get('status') != 'failed', 'document entered failed; inspect worker log and reindex after repair')
        if doc.get('status') == 'ready':
            total, indexed = doc.get('chunk_count'), doc.get('indexed_chunk_count')
            require(type(total) is int and total > 0 and total == indexed,
                    'ready document has inconsistent or empty chunk counters')
            return doc
        require(doc.get('status') == 'indexing', 'unknown document status')
        require(time.monotonic() < deadline, f'indexing did not converge within {timeout}s')
        time.sleep(1)


def multipart_body(content):
    boundary = 'eino-acceptance-' + uuid.uuid4().hex
    body = (f'--{boundary}\r\nContent-Disposition: form-data; name="visibility"\r\n\r\nsystem\r\n'
            f'--{boundary}\r\nContent-Disposition: form-data; name="file"; filename="p04-upload.md"\r\n'
            f'Content-Type: text/markdown; charset=utf-8\r\n\r\n{content}\r\n--{boundary}--\r\n')
    return body.encode(), f'multipart/form-data; boundary={boundary}'


def search_contains(client, dataset_path, document_id, marker, absent=None):
    result = client.request('POST', dataset_path + '/search', {'query': marker, 'top_k': 5})
    for field in ('data', 'channels', 'degraded', 'top_k', 'granularity', 'matched_chunks', 'chunk_budget'):
        require(field in result, f'search response missing {field}')
    hits = [hit for hit in result['data'] if hit.get('document_id') == document_id]
    require(hits and any(marker in hit.get('content', '') for hit in hits), 'expected document/content is not searchable')
    if absent:
        require(all(absent not in hit.get('content', '') for hit in hits), 'old body remains in settled search results')


def run(client, args, report, ledger):
    def checkpoint(message):
        ledger['checkpoints'].append(message)
        save_json(report / 'resources.json', ledger)
        print('PASS: ' + message, flush=True)

    name = 'acceptance-p04-' + uuid.uuid4().hex
    ds = client.request('POST', '/dataset', {'name': name, 'description': 'P03/P04 isolated acceptance',
                                             'type': 'document', 'visibility': 'system'})
    dataset_id = ds.get('id')
    require(type(dataset_id) is int and dataset_id > 0, 'dataset response missing positive id')
    ledger.update(dataset_id=dataset_id, dataset_name=name)
    checkpoint('created a unique document dataset')
    dataset_path = f'/dataset/{dataset_id}'
    docs_path = dataset_path + '/documents'
    body_a = '# 生命周期验收\n\n验收蓝鲸词A，用于第一次索引。'
    body_b = '# 生命周期验收更新\n\n验收蓝鲸词B，仅保留更新后的内容。'
    request_a = {'title': name + '-A', 'content': body_a, 'visibility': 'system'}

    if args.expect_queue_outage:
        failure = client.request('POST', docs_path, request_a, expected=503)
        require(failure.get('code') == 'service_unavailable', 'publish failure must have explicit unavailable code')
        documents = client.request('GET', docs_path)['data']
        matches = [doc for doc in documents if doc.get('title') == request_a['title']]
        require(len(matches) == 1 and matches[0].get('status') == 'failed', 'publish failure did not leave a visible failed document')
        doc_id = matches[0]['id']
        ledger['document_ids'].append(doc_id)
        checkpoint('publish outage returned 503 and persisted failed (not a false success)')
        print('现在请恢复隔离 Redis 和 worker；脚本将有界重试 reindex。', flush=True)
        deadline = time.monotonic() + args.timeout
        while True:
            response = client.request('POST', docs_path + f'/{doc_id}/reindex', expected=(200, 503))
            if response.get('code') != 'service_unavailable':
                break
            require(time.monotonic() < deadline, 'queue recovery/reindex timed out')
            time.sleep(2)
        require(response.get('id') == doc_id, 'reindex returned a different document')
    else:
        created = client.request('POST', docs_path, request_a)
        require(len(created.get('data', [])) == 1, 'ordinary document creation must return one item')
        doc = created['data'][0]
        doc_id = doc.get('id')
        require(type(doc_id) is int and doc_id > 0, 'created document missing id')
        ledger['document_ids'].append(doc_id)
        require(doc.get('operation') == 'created' and doc.get('status') in ('indexing', 'ready'), 'unexpected create operation/status')
    checkpoint('document ID recorded; creation is asynchronous')
    doc_path = docs_path + f'/{doc_id}'
    if args.expect_paused_worker:
        for _ in range(5):
            pending = client.request('GET', doc_path)
            require(pending.get('status') == 'indexing' and pending.get('chunk_count') == 0,
                    'paused worker must not report ready or produce chunks')
            time.sleep(1)
        checkpoint('paused worker left document indexing with zero chunks for five seconds')
        print('现在请启动隔离 worker；脚本将等待 ready。', flush=True)
    ready(client, doc_path, args.timeout)
    content = client.request('GET', doc_path + '/content')
    require(content.get('content') == body_a, 'stored body A differs from submitted content')
    search_contains(client, dataset_path, doc_id, '验收蓝鲸词A')
    checkpoint('ready counters agree; body A roundtrips and is searchable')
    error = client.request('POST', docs_path, {'title': 'invalid-empty-body', 'content': ' '}, expected=400)
    require(error.get('code') == 'bad_request', 'empty content must return bad_request')
    error = client.request('POST', dataset_path + '/search', {'query': ' '}, expected=400)
    require(error.get('code') == 'bad_request', 'empty query must return bad_request')
    checkpoint('invalid content/query returned clear 400 errors')
    updated = client.request('PUT', doc_path, {'content': body_b})
    require(updated.get('id') == doc_id and updated.get('status') in ('indexing', 'ready'), 'update response drift')
    ready(client, doc_path, args.timeout)
    content = client.request('GET', doc_path + '/content')
    require(content.get('content') == body_b, 'stored body B differs from submitted content')
    search_contains(client, dataset_path, doc_id, '验收蓝鲸词B', absent='验收蓝鲸词A')
    checkpoint('update converged; body/search return B and no old A content')
    rebuilt = client.request('POST', doc_path + '/reindex')
    require(rebuilt.get('id') == doc_id and rebuilt.get('status') in ('indexing', 'ready'), 'single reindex response drift')
    ready(client, doc_path, args.timeout)
    search_contains(client, dataset_path, doc_id, '验收蓝鲸词B')
    checkpoint('single-document reindex converged')
    uploaded_body = '# multipart验收\n\n验收海豚词C，第二篇文档。'
    uploaded = client.request('POST', docs_path + '/upload', multipart=multipart_body(uploaded_body))
    require(len(uploaded.get('data', [])) == 1, 'multipart ordinary document response drift')
    upload_id = uploaded['data'][0].get('id')
    require(type(upload_id) is int and upload_id > 0 and upload_id != doc_id, 'upload document ID missing')
    ledger['document_ids'].append(upload_id)
    checkpoint('multipart file field accepted; second document ID recorded')
    upload_path = docs_path + f'/{upload_id}'
    ready(client, upload_path, args.timeout)
    require(client.request('GET', upload_path + '/content').get('content') == uploaded_body, 'multipart body roundtrip failed')
    result = client.request('POST', dataset_path + '/reindex')
    require(result.get('status') == 'accepted' and result.get('documents') == 2 and result.get('failed') == 0 and result.get('chunks') == 0,
            'dataset reindex must report accepted, two queued, zero failed and compatibility chunks=0')
    ready(client, doc_path, args.timeout)
    ready(client, upload_path, args.timeout)
    search_contains(client, dataset_path, doc_id, '验收蓝鲸词B')
    search_contains(client, dataset_path, upload_id, '验收海豚词C')
    checkpoint('dataset reindex converged for both documents')
    require(client.request('DELETE', doc_path).get('status') == 'deleted', 'delete response drift')
    ledger['deleted_document_ids'].append(doc_id)
    for path in (doc_path, doc_path + '/content'):
        error = client.request('GET', path, expected=404)
        require(error.get('code') == 'not_found', 'deleted document must return not_found')
    hits = client.request('POST', dataset_path + '/search', {'query': '验收蓝鲸词B', 'top_k': 5})['data']
    require(all(hit.get('document_id') != doc_id for hit in hits), 'deleted document is still searchable')
    checkpoint('deleted document has no detail/body/search result')


def cleanup(client, ledger):
    dataset_id = ledger.get('dataset_id')
    if not dataset_id:
        return
    path = f'/dataset/{dataset_id}'
    ds = client.request('GET', path)
    require(ds.get('name') == ledger['dataset_name'] and ds.get('name', '').startswith('acceptance-p04-'),
            'refusing cleanup: dataset identity differs from this run')
    for doc_id in ledger['document_ids']:
        if doc_id not in ledger['deleted_document_ids']:
            client.request('DELETE', path + f'/documents/{doc_id}')
            ledger['deleted_document_ids'].append(doc_id)
    remaining = client.request('GET', path + '/documents')['data']
    require(not remaining, 'refusing dataset deletion: contains untracked documents')
    client.request('DELETE', path)
    ledger['dataset_deleted'] = True


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--base', default=os.environ.get('EINO_ACCEPTANCE_BASE', 'http://127.0.0.1:8090/api/v1'))
    parser.add_argument('--timeout', type=int, default=120, help='bounded wait per indexing/recovery phase, seconds')
    parser.add_argument('--cleanup', action='store_true', help='delete only this run\'s tracked resources after execution')
    modes = parser.add_mutually_exclusive_group()
    modes.add_argument('--expect-paused-worker', action='store_true')
    modes.add_argument('--expect-queue-outage', action='store_true')
    args = parser.parse_args()
    root = Path(__file__).resolve().parents[2]
    run_id = datetime.datetime.now().strftime('%Y%m%d-%H%M%S') + '-' + uuid.uuid4().hex[:8]
    report = root / 'logs/acceptance/P04' / run_id
    report.mkdir(parents=True)
    ledger = {'run_id': run_id, 'document_ids': [], 'deleted_document_ids': [], 'checkpoints': []}
    client = None
    status, code, message = 'FAIL', 1, ''
    try:
        if os.environ.get('EINO_ACCEPTANCE_ISOLATED') != '1':
            raise Blocked('set EINO_ACCEPTANCE_ISOLATED=1 only AFTER verifying dedicated PG/Redis/Milvus/ES resources')
        token = os.environ.get('EINO_ACCEPTANCE_TOKEN')
        if not token:
            raise Blocked('EINO_ACCEPTANCE_TOKEN is required; use a test administrator credential')
        url = urllib.parse.urlsplit(args.base)
        if url.scheme not in ('http', 'https') or not url.hostname or url.username or url.password or url.query or url.fragment:
            raise Blocked('base must be a clean HTTP(S) API URL, without credentials, query or fragment')
        if args.timeout <= 0:
            raise Blocked('timeout must be positive')
        ledger['base'] = args.base
        client = Client(args.base, token, report)
        run(client, args, report, ledger)
        status, code, message = 'PASS', 0, 'Live P03/P04 HTTP lifecycle passed; inspect stored HTTP responses for evidence'
    except Blocked as exc:
        status, code, message = 'BLOCKED', 2, str(exc)
    except (Failed, KeyError, IndexError, TypeError, ValueError) as exc:
        message = str(exc) or type(exc).__name__
    except KeyboardInterrupt:
        message = 'interrupted; resources preserved in ledger'
    finally:
        if args.cleanup and client is not None:
            try:
                cleanup(client, ledger)
            except (Blocked, Failed, KeyError, IndexError, TypeError, ValueError) as exc:
                ledger['cleanup_error'] = str(exc)
                status, code, message = 'FAIL', 1, 'cleanup did not complete; resources are recorded for manual review'
        save_json(report / 'resources.json', ledger)
        save_json(report / 'summary.json', {'plan': 'P04', 'mode': 'live', 'status': status, 'message': message,
                                           'note': 'External-store chunk row/idempotence evidence is also required; see docs/p03-p04-acceptance.md'})
    print(f'{status}: {message}\n报告: {report}', flush=True)
    return code


if __name__ == '__main__':
    sys.exit(main())
