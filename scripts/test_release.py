"""Exercise promotion failures without credentials, a daemon or public writes."""
import copy
import json
import os
from pathlib import Path
import tempfile
import subprocess
import unittest
from unittest.mock import patch
import urllib.error

import release as r

SOURCE = "a" * 40
NEW = "sha256:" + "b" * 64
OLD = "sha256:" + "c" * 64
REPOS = ["ghcr.io/example/dnsweaver", "docker.io/example/dnsweaver"]


class FakeRegistry:
    def __init__(self, plan):
        self.values = {plan["image"]: NEW}
        for repo in REPOS:
            self.values[repo + ":latest"] = OLD
            self.values[repo + "@" + OLD] = OLD
        self.writes = []
        self.fail = None

    def resolve(self, ref):
        return self.values[ref]

    def copy(self, source, destination):
        self.values[destination] = self.resolve(source)
        self.writes.append(destination)
        # Model an ambiguous failure AFTER the registry accepted the write.
        if self.fail == destination:
            self.fail = None
            raise RuntimeError("interrupted response")

    def immutable_copy(self, source, destination):
        if destination in self.values:
            if self.values[destination] != self.resolve(source):
                raise ValueError("conflicting immutable tag")
            return
        self.copy(source, destination)


class FakeGitHub:
    def __init__(self):
        self.source = SOURCE
        self.latest_id = 1
        self.fail = None
        self.release = {"id": 2, "draft": True}

    def tag_commit(self, version):
        return self.source

    def prepare(self, plan, notes, assets):
        if self.fail == "upload":
            raise RuntimeError("upload failed")
        return self.release

    def publish(self, release):
        if self.fail == "publish":
            raise RuntimeError("publish failed")
        release["draft"] = False

    def latest(self):
        return self.latest_id

    def set_latest(self, release_id):
        self.latest_id = release_id
        if self.fail == "latest":
            self.fail = None
            raise RuntimeError("latest response failed")


class PromotionTests(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self.tmp.cleanup)
        self.journal = Path(self.tmp.name) / "publication.json"
        self.plan = {"source": SOURCE, "version": "v3.0.0", "image": "staging.invalid/dnsweaver@" + NEW, "digest": NEW}
        self.registry = FakeRegistry(self.plan)
        self.github = FakeGitHub()

    def promote(self):
        r.promote(self.plan, REPOS, self.github, self.registry, "notes", [], self.journal)

    def assert_latest(self, wanted):
        for repo in REPOS:
            self.assertEqual(self.registry.resolve(repo + ":latest"), wanted)

    def test_success_and_retry(self):
        self.promote()
        self.assert_latest(NEW)
        self.assertEqual(self.github.latest(), 2)
        writes = list(self.registry.writes)
        self.promote()
        self.assertEqual(self.registry.writes, writes + [p + ":latest" for p in REPOS])
        self.assertEqual(r.read_json(self.journal)["phase"], "complete")

    def test_tag_mismatch_has_no_writes(self):
        self.github.source = "d" * 40
        with self.assertRaises(ValueError):
            self.promote()
        self.assertFalse(self.registry.writes)

    def test_conflicting_version_never_overwritten(self):
        destination = REPOS[0] + ":v3.0.0"
        self.registry.values[destination] = OLD
        with self.assertRaises(ValueError):
            self.promote()
        self.assert_latest(OLD)
        self.assertEqual(self.registry.values[destination], OLD)
        self.assertTrue(self.github.release["draft"])

    def test_upload_or_publication_failure_leaves_latest(self):
        for failure in ("upload", "publish"):
            with self.subTest(failure=failure):
                self.github.fail = failure
                with self.assertRaises(RuntimeError):
                    self.promote()
                self.assert_latest(OLD)
                self.assertEqual(self.github.latest(), 1)
        self.github.fail = None
        self.promote()
        self.assert_latest(NEW)

    def test_latest_partial_write_rolls_back_both_registries(self):
        self.registry.fail = REPOS[1] + ":latest"
        with self.assertRaises(RuntimeError):
            self.promote()
        self.assert_latest(OLD)
        self.assertEqual(self.github.latest(), 1)
        self.assertEqual(r.read_json(self.journal)["rollback_failures"], [])
        self.promote()
        self.assert_latest(NEW)

    def test_github_latest_failure_rolls_back_every_surface(self):
        self.github.fail = "latest"
        with self.assertRaises(RuntimeError):
            self.promote()
        self.assert_latest(OLD)
        self.assertEqual(self.github.latest(), 1)

    def test_prerelease_never_changes_latest(self):
        self.plan["version"] = "v3.0.0-rc.1"
        self.promote()
        self.assert_latest(OLD)
        self.assertEqual(self.github.latest(), 1)

    def test_missing_rollback_target_fails_before_public_writes(self):
        del self.registry.values[REPOS[1] + ":latest"]
        with self.assertRaises(KeyError):
            self.promote()
        self.assertFalse(self.registry.writes)

    def test_digest_mismatch_fails_before_public_writes(self):
        self.registry.values[self.plan["image"]] = OLD
        with self.assertRaises(ValueError):
            self.promote()
        self.assertFalse(self.registry.writes)


class QualificationTests(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self.tmp.cleanup)
        self.directory = Path(self.tmp.name)
        self.records = {}
        for arch in r.ARCHES:
            folder = self.directory / arch
            folder.mkdir()
            (folder / f"sbom-{arch}.cdx.json").write_text('{}\n')
            (folder / "scan.private.json").write_text('{}\n')
            record = {"source": SOURCE, "version": "v3.0.0", "arch": arch, "digest": NEW,
                "image": "staging.invalid/dnsweaver@" + NEW,
                "inputs": r.read_json(r.ROOT / "ci/release-inputs.json"),
                "runtime": {"architecture": arch, "version": "v3.0.0", "health": "passed", "pending_provider_readiness": "passed", "loopback_and_network_opt_in": "passed"},
                "scan": {"status": "passed", "ignore_sha256": r.digest((r.ROOT / ".trivyignore").read_bytes()), "report_sha256": r.digest(b'{}\n')},
                "sbom": {"file": f"sbom-{arch}.cdx.json", "sha256": r.digest(b'{}\n')}}
            self.records[arch] = record
            r.write_json(folder / "qualified.json", record)

    def check(self):
        return r.qualification(self.directory, SOURCE, "v3.0.0")

    def test_complete_candidate(self):
        self.assertEqual(len(self.check()), 2)

    def test_tampered_identity_runtime_inputs_policy_and_path(self):
        changes = [("source", "d" * 40), ("version", "v2.8.2"), ("arch", "amd64"),
            ("digest", "not-a-digest"), ("image", "staging.invalid/dnsweaver:mutable"),
            ("runtime", {**self.records["arm64"]["runtime"], "health": "failed"}),
            ("runtime", {**self.records["arm64"]["runtime"], "architecture": "amd64"}),
            ("inputs", {**self.records["arm64"]["inputs"], "builder": "golang:latest"}),
            ("scan", {**self.records["arm64"]["scan"], "ignore_sha256": OLD}),
            ("sbom", {"file": "../amd64/sbom-amd64.cdx.json", "sha256": r.digest(b'{}\n')})]
        for key, value in changes:
            with self.subTest(key=key, value=value):
                record = copy.deepcopy(self.records["arm64"])
                record[key] = value
                r.write_json(self.directory / "arm64/qualified.json", record)
                with self.assertRaises(ValueError):
                    self.check()

    def test_artifact_tampering(self):
        for filename in ("sbom-arm64.cdx.json", "scan.private.json"):
            with self.subTest(filename=filename):
                path = self.directory / "arm64" / filename
                path.write_text("tampered")
                with self.assertRaises(ValueError):
                    self.check()
                path.write_text('{}\n')


class SourceIdentityTests(unittest.TestCase):
    def test_dirty_or_untracked_build_input_is_rejected(self):
        for status in (" M Dockerfile", "?? injected.go"):
            with self.subTest(status=status):
                results = [subprocess.CompletedProcess([], 0, SOURCE + "\n", ""),
                           subprocess.CompletedProcess([], 0, status + "\n", "")]
                with patch.dict(os.environ, {"CI_COMMIT_SHA": SOURCE, "CI_COMMIT_TAG": ""}):
                    with patch.object(r, "run", side_effect=results):
                        with self.assertRaises(ValueError):
                            r.source_identity()

    def test_clean_source_reports_the_bound_branch_version(self):
        results = [subprocess.CompletedProcess([], 0, SOURCE + "\n", ""),
                   subprocess.CompletedProcess([], 0, "", "")]
        with patch.dict(os.environ, {"CI_COMMIT_SHA": SOURCE, "CI_COMMIT_TAG": ""}):
            with patch.object(r, "run", side_effect=results):
                self.assertEqual(r.source_identity(), (SOURCE, "sha-" + SOURCE[:12]))


class RuntimeOwnershipTests(unittest.TestCase):
    def test_failed_create_never_removes_an_existing_container(self):
        calls = []

        def fake_run(*args, **kwargs):
            calls.append(args)
            if args[:2] == ("docker", "create"):
                raise RuntimeError("container name already exists")
            output = ""
            if args[:3] == ("docker", "image", "inspect"):
                output = "amd64\n"
            elif args[-1] == "--version":
                output = "dnsweaver v3.0.0 (built test)\n"
            return subprocess.CompletedProcess(args, 0, output, "")

        with patch.object(r, "run", side_effect=fake_run):
            with self.assertRaises(RuntimeError):
                r.runtime_checks("test@" + NEW, "amd64", "v3.0.0")
        self.assertFalse(any(c[:2] == ("docker", "rm") for c in calls))


    def test_shell_healthcheck_does_not_clean_up_failed_create(self):
        with tempfile.TemporaryDirectory() as directory:
            folder = Path(directory)
            docker = folder / "docker"
            log = folder / "calls.log"
            docker.write_text('#!/bin/sh\nprintf "%s\\n" "$*" >> "$TEST_DOCKER_LOG"\nexit 1\n')
            docker.chmod(0o755)
            environment = {**os.environ, "PATH": str(folder) + os.pathsep + os.environ["PATH"], "TEST_DOCKER_LOG": str(log)}
            result = subprocess.run(["sh", str(r.ROOT / "scripts/test-image-healthcheck.sh"), "fixture"],
                                    env=environment, capture_output=True, text=True, check=False)
            self.assertNotEqual(result.returncode, 0)
            calls = log.read_text().splitlines()
            self.assertEqual(len(calls), 1)
            self.assertTrue(calls[0].startswith("create "))


class GitHubAPITests(unittest.TestCase):
    def setUp(self):
        self.client = r.GitHub("example/dnsweaver", "fake-test-token")
        self.plan = {"version": "v3.0.0", "source": SOURCE}
        self.draft = {"id": 2, "tag_name": "v3.0.0", "body": "notes", "draft": True,
                      "prerelease": False, "upload_url": "https://uploads.github.com/repos/example/dnsweaver/releases/2/assets{?name}"}

    def test_retry_finds_existing_draft(self):
        missing = urllib.error.HTTPError("https://api.github.com", 404, "not found", {}, None)
        with patch.object(self.client, "request", side_effect=[missing, [self.draft], []]) as request:
            self.assertEqual(self.client.prepare(self.plan, "notes", []), self.draft)
            self.assertTrue(all(call.args[1:] == () for call in request.call_args_list))

    def test_asset_upload_is_verified_and_existing_mismatch_refused(self):
        with tempfile.TemporaryDirectory() as directory:
            asset = Path(directory) / "evidence.json"
            asset.write_text('{}')
            with patch.object(self.client, "request", side_effect=[self.draft, [], {"digest": r.digest(b'{}')}]) as request:
                self.client.prepare(self.plan, "notes", [asset])
                self.assertEqual(request.call_args_list[-1].args[1], "POST")
            with patch.object(self.client, "request", side_effect=[self.draft, [{"name": asset.name, "digest": OLD}]]):
                with self.assertRaises(ValueError):
                    self.client.prepare(self.plan, "notes", [asset])
            published = {**self.draft, "draft": False}
            with patch.object(self.client, "request", side_effect=[published, []]):
                with self.assertRaises(ValueError):
                    self.client.prepare(self.plan, "notes", [asset])

    def test_api_does_not_accept_arbitrary_hosts(self):
        with self.assertRaises(ValueError):
            self.client.request("https://example.invalid/collect")


if __name__ == "__main__":
    unittest.main()
