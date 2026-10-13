#!/usr/bin/env python3
"""Authenticated workflow proof across an actual restart on disposable CI data."""
import http.cookiejar
import json
import os
import pathlib
import sys
import time
import urllib.request

mode, base, directory = sys.argv[1:]
directory = pathlib.Path(directory)
directory.mkdir(parents=True, exist_ok=True)
jar = http.cookiejar.MozillaCookieJar(str(directory / "workflow-cookies"))
client = urllib.request.build_opener(urllib.request.HTTPCookieProcessor(jar))
proof_path = directory / "workflow-proof.json"


def call(path, body=None, method=None, key=None):
    headers = {"Content-Type": "application/json"}
    csrf = next((c.value for c in jar if c.name == "opensnow_csrf"), None)
    if csrf:
        headers["X-CSRF-Token"] = csrf
    if key:
        headers["Idempotency-Key"] = key
    request = urllib.request.Request(base + "/api/v1" + path,
        data=None if body is None else json.dumps(body).encode(),
        headers=headers, method=method)
    with client.open(request, timeout=15) as response:
        data = response.read()
        return json.loads(data) if data else None


def poll(read, matches):
    for _ in range(120):
        result = read()
        if matches(result):
            return result
        time.sleep(1)
    raise AssertionError("Workflow did not reach expected durable state")


if mode == "create":
    call("/auth/login", dict(tenant=os.environ.get("E2E_TENANT", "e2e"),
        email=os.environ.get("E2E_ADMIN_EMAIL", "admin"),
        password=os.environ["WORKFLOW_PROOF_PASSWORD"]))
    me = call("/auth/me")
    user = me["user"]["id"]
    group = call("/admin/rbac/groups", {"name": "Workflow restart proof"})["id"]
    call(f"/admin/rbac/groups/{group}/members/{user}", method="POST")
    nodes = [
        dict(id="start", label="Start", type="START", next="approval"),
        dict(id="approval", label="Decision", type="APPROVAL", target_type="USER",
            target_id=user, policy="ANY", seconds=3600, next="task", rejected="cancel", timeout="cancel"),
        dict(id="task", label="Task", type="CREATE_TASK", title="Restart proof task", next="wait"),
        dict(id="wait", label="Wait", type="WAIT_TASK", task_ref="task", seconds=3600,
            next="complete", timeout="cancel"),
        dict(id="complete", label="Complete", type="UPDATE_RECORD", state="FULFILLED",
            reason="Required task completed", next="notify"),
        dict(id="cancel", label="Cancel", type="UPDATE_RECORD", state="CANCELLED",
            reason="Approval rejected or timed out", next="notify"),
        dict(id="notify", label="Notify", type="NOTIFY", audience="REQUESTER", next="end"),
        dict(id="end", label="End", type="END"),
    ]
    definition = call("/workflow/definitions", {"draft": dict(name="Restart proof",
        executor_id=user, group_id=group, self_approval="ALLOW", nodes=nodes)}, key="workflow-runtime-definition")
    published = call(f"/workflow/definitions/{definition['id']}/publish", {"version": definition["version"]})
    catalog = call("/catalog/admin/catalogs", {"name": "Restart proof"}, key="workflow-runtime-catalog")
    category = call("/catalog/admin/categories", {"name": "Restart proof", "catalog_id": catalog["id"]}, key="workflow-runtime-category")
    item = call("/catalog/admin/items", {"category_id": category["id"], "draft": dict(
        title="Restart proof service", description="Disposable runtime verification", fields=[],
        method="WORKFLOW", group_id=group, steps=[], workflow_id=definition["id"],
        workflow_version=published["published_version"])}, key="workflow-runtime-item")
    item = call(f"/catalog/admin/items/{item['id']}/publish", {"version": item["version"]})
    submission = {"items": [{"item_id": item["id"], "version": item["published_version"], "values": {}}]}
    request = call("/requests/", submission, key="workflow-runtime-submission")
    assert request["items"][0]["total_tasks"] == 0
    execution = poll(lambda: call(f"/workflow/executions?request_id={request['id']}")["items"][0],
        lambda x: x["state"] == "WAITING" and x["current_node"] == "approval")
    execution = call(f"/workflow/executions/{execution['id']}")
    step = next(s for s in execution["steps"] if s["node_id"] == "approval")
    proof_path.write_text(json.dumps(dict(request=request, execution=execution, step=step, submission=submission)))
    jar.save(ignore_discard=True, ignore_expires=True)
    print("Durable approval waits before restart; no fulfillment task exists")
elif mode == "verify":
    jar.load(ignore_discard=True, ignore_expires=True)
    proof = json.loads(proof_path.read_text())
    request = call(f"/requests/{proof['request']['id']}")
    assert request["number"] == proof["request"]["number"]
    assert request["version"] == proof["request"]["version"]
    assert request["items"][0]["total_tasks"] == 0
    path = f"/workflow/executions/{proof['execution']['id']}"
    execution = call(path)
    assert execution["state"] == "WAITING"
    assert execution["definition_version"] == proof["execution"]["definition_version"]
    step = next(s for s in execution["steps"] if s["node_id"] == "approval")
    assert (step["deadline"], step["version"]) == (proof["step"]["deadline"], proof["step"]["version"])
    decision = dict(version=step["version"], decision="APPROVED", reason="Authenticated decision after real restart")
    call(path + "/decisions/approval", decision)
    call(path + "/decisions/approval", decision)
    request = poll(lambda: call(f"/requests/{request['id']}"), lambda x: x["items"][0]["total_tasks"] == 1)
    task = next(t for t in call("/fulfillment/?view=group&limit=100")["items"] if t["request_id"] == request["id"])
    assert task["title"] == "Restart proof task"
    print("Saved session, pinned version/deadline, decision retry and exactly one real task verified after restart")
else:
    raise ValueError("Expected create or verify")
