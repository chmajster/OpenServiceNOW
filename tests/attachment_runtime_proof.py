#!/usr/bin/env python3
"""Real API/storage proof for a disposable Docker lifecycle fixture only."""
import hashlib
import http.cookiejar
import json
import os
import pathlib
import sys
import urllib.error
import urllib.request

mode, base, directory = sys.argv[1:]
directory = pathlib.Path(directory)
jar = http.cookiejar.MozillaCookieJar(str(directory / 'file-proof-cookies'))
client = urllib.request.build_opener(urllib.request.HTTPCookieProcessor(jar))
proof_path = directory / 'file-proof.json'
if mode == 'create':
    body = json.dumps(dict(tenant='e2e', email='attachment-proof@example.test', password=os.environ['ATTACHMENT_PROOF_PASSWORD'])).encode()
    login = urllib.request.Request(base + '/api/v1/auth/login', data=body, headers={'Content-Type': 'application/json'})
    assert client.open(login).status == 200
    csrf = next(cookie.value for cookie in jar if cookie.name == 'opensnow_csrf')
    headers = {'Content-Type': 'application/json', 'X-CSRF-Token': csrf, 'Idempotency-Key': 'container-file-proof-incident'}
    fields = dict(title='Container storage proof', description='Persistent attachment bytes', category='other', subcategory='other', impact=3, urgency=3)
    request = urllib.request.Request(base + '/api/v1/incidents/', data=json.dumps(fields).encode(), headers=headers)
    record = json.load(client.open(request))
    content = b'container-persistent-bytes\n' * 125000  # > original 2MiB proxy limit
    boundary = 'opensnow-storage-proof-boundary'
    def multipart(data):
        return (f'--{boundary}\r\nContent-Disposition: form-data; name="visibility"\r\n\r\nPUBLIC\r\n'
                f'--{boundary}\r\nContent-Disposition: form-data; name="file"; filename="trwaly.txt"\r\nContent-Type: text/plain\r\n\r\n').encode() + data + f'\r\n--{boundary}--\r\n'.encode()
    upload_url = base + '/api/v1/incidents/' + record['id'] + '/files'
    upload_headers = {'Content-Type': 'multipart/form-data; boundary=' + boundary, 'X-CSRF-Token': csrf, 'Idempotency-Key': 'container-file-proof-upload'}
    file = json.load(client.open(urllib.request.Request(upload_url, data=multipart(content), headers=upload_headers)))
    assert 'storage_key' not in file and 'path' not in file
    for size in [10 * 1024 * 1024 + 1, 11 * 1024 * 1024 + 1]:
        try:
            client.open(urllib.request.Request(upload_url, data=multipart(b'x' * size), headers=upload_headers))
            raise AssertionError('Oversized upload accepted')
        except urllib.error.HTTPError as error:
            assert error.code == 413
            assert json.load(error)['error']['code'] == 'FILE_TOO_LARGE'
    proof_path.write_text(json.dumps(dict(incident_id=record['id'], file=file, sha256=hashlib.sha256(content).hexdigest())))
    jar.save(ignore_discard=True, ignore_expires=True)
else:
    jar.load(ignore_discard=True, ignore_expires=True)
proof = json.loads(proof_path.read_text())
url = base + '/api/v1/incidents/' + proof['incident_id'] + '/files'
listing = json.load(client.open(url))
assert listing['files'] == [proof['file']]
response = client.open(url + '/' + proof['file']['id'])
assert response.headers['Content-Disposition'].startswith('attachment;')
assert hashlib.sha256(response.read()).hexdigest() == proof['sha256']
print('Attachment metadata/session/bytes verified through authenticated API:', mode)
