"""Run the mirror workflow against local Git fixtures, without network or credentials."""

import os
import re
import shutil
import subprocess
import tempfile
import textwrap
import unittest
from pathlib import Path


class MirrorWorkflowTests(unittest.TestCase):
    def setUp(self) -> None:
        self.tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self.tmp.cleanup)
        self.root = Path(self.tmp.name)
        self.source = self.root / "source"
        self.mirror = self.root / "mirror.git"
        self.runner = self.root / "runner"
        self.runner.mkdir()
        self.git("init", "--quiet", "--initial-branch=main", str(self.source))
        self.git("init", "--quiet", "--bare", str(self.mirror))
        self.first = self.commit("first")
        self.git(
            "-C", str(self.source), "push", str(self.mirror), "HEAD:refs/heads/main"
        )
        self.second = self.commit("second")

        workflow = (
            Path(__file__).resolve().parents[1] / ".github/workflows/sync-to-gitlab.yml"
        )
        # Extract this workflow's two literal shell blocks. Fixtures execute
        # the committed code; a layout change must update the extraction too.
        blocks = re.findall(
            r"^        run: \|\n((?:^          .*\n|^\n)+)",
            workflow.read_text(),
            re.MULTILINE,
        )
        self.assertEqual(len(blocks), 2)
        self.steps = [textwrap.dedent(block) for block in blocks]

        shim = self.root / "bin"
        shim.mkdir()
        wrapper = shim / "git"
        wrapper.write_text(
            "#!/usr/bin/env python3\n"
            "import os, subprocess, sys\n"
            "args = sys.argv[1:]\n"
            "for value in ('test-github-token', 'test-gitlab-token'):\n"
            "    if any(value in arg for arg in args):\n"
            "        sys.exit('credential entered argv')\n"
            "for index, arg in enumerate(args):\n"
            "    if arg.startswith('https://github.com/'):\n"
            "        assert os.environ['GIT_CONFIG_KEY_0'] == 'http.https://github.com/.extraheader'\n"
            "        args[index] = os.environ['FIXTURE_SOURCE']\n"
            "    elif arg.startswith('https://mirror.example.com/'):\n"
            "        assert os.environ['GIT_CONFIG_KEY_0'] == 'http.https://mirror.example.com/.extraheader'\n"
            "        if os.environ.get('FIXTURE_READ_FAILURE') and args[0] == 'ls-remote':\n"
            "            sys.exit(128)\n"
            "        args[index] = os.environ['FIXTURE_MIRROR']\n"
            "sys.exit(subprocess.run([os.environ['FIXTURE_GIT'], *args], timeout=10).returncode)\n"
        )
        wrapper.chmod(0o755)
        self.env = {
            **os.environ,
            "PATH": str(shim) + os.pathsep + os.environ["PATH"],
            "FIXTURE_GIT": shutil.which("git") or "git",
            "FIXTURE_SOURCE": str(self.source),
            "FIXTURE_MIRROR": str(self.mirror),
            "GITHUB_REPOSITORY": "example/dnsweaver",
            "GITHUB_REF": "refs/heads/main",
            "GITHUB_REF_NAME": "main",
            "GITHUB_SHA": self.second,
            "READ_TOKEN": "test-github-token",
            "GITLAB_TOKEN": "test-gitlab-token",
            "GITLAB_HOST": "mirror.example.com",
            "GITLAB_PROJECT_PATH": "example/dnsweaver",
            # Ignore global/system Git settings and hooks in these fixtures.
            "GIT_CONFIG_GLOBAL": os.devnull,
            "GIT_CONFIG_NOSYSTEM": "1",
        }

    def git(self, *args: str) -> str:
        result = subprocess.run(
            [
                "git",
                "-c",
                "user.name=Fixture",
                "-c",
                "user.email=fixture@example.com",
                *args,
            ],
            check=True,
            text=True,
            capture_output=True,
            timeout=10,
            env={
                **os.environ,
                "GIT_CONFIG_GLOBAL": os.devnull,
                "GIT_CONFIG_NOSYSTEM": "1",
            },
        )
        return result.stdout.strip()

    def commit(self, content: str) -> str:
        (self.source / "content.txt").write_text(content)
        self.git("-C", str(self.source), "add", "--", "content.txt")
        self.git(
            "-C",
            str(self.source),
            "commit",
            "--quiet",
            "-m",
            content,
            "--",
            "content.txt",
        )
        return self.git("-C", str(self.source), "rev-parse", "HEAD")

    def run_step(self, index: int) -> subprocess.CompletedProcess[str]:
        return subprocess.run(
            ["bash"],
            check=False,
            input=self.steps[index],
            cwd=self.runner,
            env=self.env,
            text=True,
            capture_output=True,
            timeout=20,
        )

    def fetch_source(self) -> None:
        result = self.run_step(0)
        self.assertEqual(result.returncode, 0, result.stderr)

    def mirror_ref(self, ref: str) -> str:
        return self.git("--git-dir", str(self.mirror), "rev-parse", ref)

    def prepare_tag(self) -> None:
        self.git("-C", str(self.source), "tag", "-a", "v3.0.1", "-m", "fixture tag")
        self.env.update(GITHUB_REF="refs/tags/v3.0.1", GITHUB_REF_NAME="v3.0.1")

    def test_main_fast_forwards_without_persisting_credentials(self) -> None:
        self.fetch_source()

        result = self.run_step(1)

        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(self.mirror_ref("refs/heads/main"), self.second)
        config = (self.runner / ".git/config").read_text()
        for credential in (
            "test-github-token",
            "test-gitlab-token",
            "extraheader",
            "oauth2:",
        ):
            self.assertNotIn(credential, config)

    def test_diverged_main_is_not_overwritten(self) -> None:
        self.git(
            "-C", str(self.source), "checkout", "--quiet", "-b", "divergent", self.first
        )
        divergent = self.commit("divergent")
        self.git(
            "-C", str(self.source), "push", str(self.mirror), "HEAD:refs/heads/main"
        )
        self.git("-C", str(self.source), "checkout", "--quiet", "main")
        self.fetch_source()

        result = self.run_step(1)

        self.assertNotEqual(result.returncode, 0)
        self.assertEqual(self.mirror_ref("refs/heads/main"), divergent)

    def test_tag_retry_preserves_the_annotated_tag_object(self) -> None:
        self.prepare_tag()
        self.fetch_source()
        self.assertEqual(self.run_step(1).returncode, 0)
        original = self.mirror_ref("refs/tags/v3.0.1")

        result = self.run_step(1)

        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(self.mirror_ref("refs/tags/v3.0.1"), original)
        self.assertEqual(
            original, self.git("-C", str(self.source), "rev-parse", "refs/tags/v3.0.1")
        )
        self.assertEqual(self.mirror_ref("refs/tags/v3.0.1^{}"), self.second)

    def test_conflicting_tag_is_not_moved(self) -> None:
        self.prepare_tag()
        self.git(
            "--git-dir", str(self.mirror), "update-ref", "refs/tags/v3.0.1", self.first
        )
        self.fetch_source()

        result = self.run_step(1)

        self.assertNotEqual(result.returncode, 0)
        self.assertEqual(self.mirror_ref("refs/tags/v3.0.1"), self.first)

    def test_moved_source_tag_is_rejected_before_push(self) -> None:
        self.prepare_tag()
        self.env["GITHUB_SHA"] = self.first

        result = self.run_step(0)

        self.assertNotEqual(result.returncode, 0)
        self.assertEqual(self.mirror_ref("refs/heads/main"), self.first)

    def test_remote_read_failure_is_not_treated_as_missing_main(self) -> None:
        self.fetch_source()
        self.env["FIXTURE_READ_FAILURE"] = "1"

        result = self.run_step(1)

        self.assertNotEqual(result.returncode, 0)
        self.assertEqual(self.mirror_ref("refs/heads/main"), self.first)

    def test_manual_feature_branch_dispatch_does_not_change_main(self) -> None:
        self.git("-C", str(self.source), "branch", "feature", self.second)
        self.env.update(GITHUB_REF="refs/heads/feature", GITHUB_REF_NAME="feature")
        self.fetch_source()

        result = self.run_step(1)

        self.assertNotEqual(result.returncode, 0)
        self.assertEqual(self.mirror_ref("refs/heads/main"), self.first)

    def test_manual_nonversion_tag_dispatch_does_not_create_a_tag(self) -> None:
        self.git("-C", str(self.source), "tag", "maintenance", self.second)
        self.env.update(
            GITHUB_REF="refs/tags/maintenance", GITHUB_REF_NAME="maintenance"
        )
        self.fetch_source()

        result = self.run_step(1)

        self.assertNotEqual(result.returncode, 0)
        self.assertEqual(self.git("--git-dir", str(self.mirror), "tag", "--list"), "")


if __name__ == "__main__":
    unittest.main()
