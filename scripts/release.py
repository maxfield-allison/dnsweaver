#!/usr/bin/env python3
"""Build, qualify and promote the same container digests without rebuilding.

Build/assemble write to the CI registry only. Publish is a separate manual job
and requires an existing public tag, qualified evidence and explicit opt-in.
Credentials are read from the environment by clients, never passed in argv.
"""

import argparse
import hashlib
import json
import os
from pathlib import Path
import re
import subprocess
import time
import urllib.error
import urllib.parse
import urllib.request

ROOT = Path(__file__).resolve().parent.parent
OUT = ROOT / "release"
ARCHES = ("amd64", "arm64")
DIGEST = re.compile(r"sha256:[0-9a-f]{64}\Z")
VERSION = re.compile(r"v[0-9]+\.[0-9]+\.[0-9]+(?:-[0-9A-Za-z.-]+)?\Z")


def run(*args, check=True):
    result = subprocess.run(args, cwd=ROOT, text=True, capture_output=True, timeout=1800, check=False)
    if check and result.returncode:
        # Commands carry no credential arguments. Tool errors may include server
        # text, so keep public logs limited to the failed operation and status.
        raise RuntimeError(f"{args[0]} {args[1]} failed (exit {result.returncode})")
    return result


def digest(data):
    return "sha256:" + hashlib.sha256(data).hexdigest()


def write_json(path, value):
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(json.dumps(value, indent=2, sort_keys=True) + "\n")


def read_json(path):
    return json.loads(path.read_text())


def env(name):
    value = os.environ.get(name, "")
    if not value:
        raise ValueError(f"{name} is required")
    return value


def source_identity():
    sha = env("CI_COMMIT_SHA")
    if not re.fullmatch(r"[0-9a-f]{40}", sha) or run("git", "rev-parse", "HEAD").stdout.strip() != sha:
        raise ValueError("build source does not match CI_COMMIT_SHA")
    version = os.environ.get("CI_COMMIT_TAG") or "sha-" + sha[:12]
    if os.environ.get("CI_COMMIT_TAG") and not VERSION.fullmatch(version):
        raise ValueError("release tag must be a semantic version")
    return sha, version


class Registry:
    def raw(self, ref):
        return run("skopeo", "inspect", "--raw", "docker://" + ref).stdout.encode()

    def resolve(self, ref):
        return digest(self.raw(ref))

    def copy(self, source, destination):
        run("skopeo", "copy", "--all", "--preserve-digests", "docker://" + source, "docker://" + destination)
        if self.resolve(destination) != self.resolve(source):
            raise RuntimeError("registry copy changed the qualified digest")

    def immutable_copy(self, source, destination):
        result = run("skopeo", "inspect", "--raw", "docker://" + destination, check=False)
        if result.returncode == 0:
            if digest(result.stdout.encode()) != self.resolve(source):
                raise RuntimeError("immutable destination tag already has different content")
            return
        # Only a missing manifest permits creation. Auth/network errors are not
        # evidence that an immutable destination is absent.
        if not any(marker in result.stderr for marker in ("manifest unknown", "MANIFEST_UNKNOWN", "name unknown")):
            raise RuntimeError("cannot establish whether destination exists")
        self.copy(source, destination)


def runtime_checks(ref, arch, version):
    run("docker", "pull", "--platform", "linux/" + arch, ref)
    actual = run("docker", "image", "inspect", ref, "--format", "{{.Architecture}}").stdout.strip()
    if actual != arch:
        raise ValueError("runtime architecture mismatch")
    reported = run("docker", "run", "--rm", "--network", "none", ref, "--version").stdout
    if not reported.startswith(f"dnsweaver {version} (built "):
        raise ValueError("image did not report the requested version")
    run("sh", "scripts/test-image-healthcheck.sh", ref)
    # Verify the actual kernel listener on each architecture, not just config
    # parsing. Use the Docker API copy path for remote runner daemons.
    for network in (False, True):
        name = f"dnsweaver-release-{os.getpid()}-{arch}-{int(network)}"
        args = ["docker", "create", "--name", name, "--network", "none"]
        if network:
            args += ["--env", "DNSWEAVER_HEALTH_ADDRESS=0.0.0.0", "--env", "DNSWEAVER_HEALTH_ALLOW_NETWORK=true"]
        args += [ref, "--config", "/tmp/probe.yml"]
        try:
            run(*args)
            run("docker", "cp", "testdata/image-healthcheck-config.yml", name + ":/tmp/probe.yml")
            run("docker", "start", name)
            for _ in range(30):
                probe = run("docker", "exec", name, "dnsweaver", "--healthcheck", check=False)
                if probe.returncode == 0:
                    break
                time.sleep(1)
            else:
                raise RuntimeError("image never became healthy")
            tcp = run("docker", "exec", name, "cat", "/proc/net/tcp", "/proc/net/tcp6").stdout
            addresses = {"00000000:46A0", "0" * 32 + ":46A0"} if network else {"0100007F:46A0"}
            if not any(len(line.split()) > 3 and line.split()[1] in addresses and line.split()[3] == "0A" for line in tcp.splitlines()):
                raise RuntimeError("management listener did not use expected address/port")
            # An unavailable startup provider remains pending. The documented
            # readiness contract is HTTP 200 with aggregate status degraded;
            # it must not report healthy or expose the upstream error.
            run("docker", "exec", name, "dnsweaver", "--readycheck")
            body = run("docker", "exec", name, "busybox", "wget", "-qO-", "http://127.0.0.1:18080/ready").stdout
            if json.loads(body) != {"status": "degraded"}:
                raise RuntimeError("pending-provider readiness was not aggregate degraded")
        finally:
            run("docker", "rm", "-f", name, check=False)
    return {"architecture": arch, "version": version, "health": "passed", "pending_provider_readiness": "passed", "loopback_and_network_opt_in": "passed"}


def build(arch):
    sha, version = source_identity()
    inputs = read_json(ROOT / "ci/release-inputs.json")
    native = run("docker", "info", "--format", "{{.Architecture}}").stdout.strip()
    if native not in {"amd64": ("amd64", "x86_64"), "arm64": ("arm64", "aarch64")}[arch]:
        raise ValueError("architecture qualification requires a native runner")
    if inputs["platforms"] != ["linux/" + a for a in ARCHES] or inputs["signing"] != "unsigned":
        raise ValueError("unsupported release platform or signing policy")
    for key in ("builder", "runtime"):
        if not DIGEST.fullmatch(inputs[key].split("@")[-1]):
            raise ValueError("release base images must be pinned by digest")
    directory = OUT / arch
    directory.mkdir(parents=True, exist_ok=True)
    repository = env("CI_REGISTRY_IMAGE")
    tag = f"{repository}:candidate-{sha}-{env('CI_PIPELINE_ID')}-{arch}"
    build_date = run("git", "show", "-s", "--format=%cI", sha).stdout.strip()
    run("docker", "buildx", "build", "--pull", "--platform", "linux/" + arch,
        "--provenance=false", "--build-arg", "BUILDER_IMAGE=" + inputs["builder"],
        "--build-arg", "RUNTIME_IMAGE=" + inputs["runtime"], "--build-arg", "VERSION=" + version,
        "--build-arg", "BUILDER_PACKAGES_SHA256=" + package_hash(inputs["builder_packages"]),
        "--build-arg", "RUNTIME_PACKAGES_SHA256=" + package_hash(inputs["runtime_packages"]),
        "--build-arg", "BUILD_DATE=" + build_date, "--build-arg", "CACHE_BUST=" + env("CI_PIPELINE_ID"),
        "--label", "org.opencontainers.image.revision=" + sha,
        "--label", "org.opencontainers.image.version=" + version,
        "--metadata-file", str(directory / "build.json"), "--tag", tag, "--push", ".")
    image_digest = read_json(directory / "build.json")["containerimage.digest"]
    if not DIGEST.fullmatch(image_digest):
        raise ValueError("builder returned an invalid digest")
    ref = repository + "@" + image_digest
    if Registry().resolve(ref) != image_digest:
        raise ValueError("registry content differs from build result")
    runtime = runtime_checks(ref, arch, version)
    write_json(directory / "image.json", {"schema": 1, "source": sha, "version": version, "arch": arch,
        "image": ref, "digest": image_digest, "inputs": inputs, "runtime": runtime})


def package_hash(packages):
    if not packages or any(not re.fullmatch(r"[A-Za-z0-9_.+~-]+", p) for p in packages):
        raise ValueError("release package set must contain explicit name-version entries")
    return hashlib.sha256(("\n".join(sorted(packages)) + "\n").encode()).hexdigest()


def scan(arch):
    directory = OUT / arch
    image = read_json(directory / "image.json")
    raw = directory / "scan.private.json"
    # A nonzero scanner exit is always fatal; the retained JSON alone cannot
    # establish completion. Existing reviewed ignores remain explicit inputs.
    run("trivy", "image", "--scanners", "vuln", "--exit-code", "1", "--severity", "HIGH,CRITICAL",
        "--ignore-unfixed", "--ignorefile", ".trivyignore", "--format", "json", "--output", str(raw), image["image"])
    sbom = directory / f"sbom-{arch}.cdx.json"
    run("trivy", "image", "--format", "cyclonedx", "--output", str(sbom), image["image"])
    # Trivy includes the private staging reference in SBOM metadata. Publish a
    # neutral digest reference instead; keep the original scan report internal.
    document = read_json(sbom)
    serialized = json.dumps(document)
    private_repo = image["image"].split("@")[0]
    public_repo = "ghcr.io/maxfield-allison/dnsweaver"
    for safe in ("", "/"):
        serialized = serialized.replace(urllib.parse.quote(private_repo, safe=safe), urllib.parse.quote(public_repo, safe=safe))
    serialized = serialized.replace(private_repo, public_repo)
    if private_repo.split("/")[0] in serialized:
        raise ValueError("SBOM still contains the private staging host")
    write_json(sbom, json.loads(serialized))
    report = read_json(raw)
    write_json(directory / "qualified.json", {**image,
        "scan": {"status": "passed", "tool": run("trivy", "--version").stdout.strip(),
                 "ignore_sha256": digest((ROOT / ".trivyignore").read_bytes()),
                 "report_sha256": digest(raw.read_bytes()), "created_at": report.get("CreatedAt")},
        "sbom": {"file": sbom.name, "sha256": digest(sbom.read_bytes())}})


def qualification(directory, source, version):
    records = []
    for arch in ARCHES:
        record = read_json(directory / arch / "qualified.json")
        if record["source"] != source or record["version"] != version or record["arch"] != arch:
            raise ValueError("qualification belongs to another candidate")
        if not DIGEST.fullmatch(record["digest"]) or not record["image"].endswith("@" + record["digest"]):
            raise ValueError("qualification is not digest-bound")
        if record["scan"]["status"] != "passed" or any(record["runtime"].get(k) != "passed" for k in ("health", "pending_provider_readiness", "loopback_and_network_opt_in")):
            raise ValueError("candidate checks did not pass")
        if record["runtime"]["architecture"] != arch or record["runtime"]["version"] != version:
            raise ValueError("runtime evidence belongs to another candidate")
        if record["inputs"] != read_json(ROOT / "ci/release-inputs.json"):
            raise ValueError("build inputs differ from committed release inputs")
        if record["scan"]["ignore_sha256"] != digest((ROOT / ".trivyignore").read_bytes()):
            raise ValueError("scan used a different exception policy")
        if record["scan"]["report_sha256"] != digest((directory / arch / "scan.private.json").read_bytes()):
            raise ValueError("scan report does not match qualification")
        sbom = directory / arch / record["sbom"]["file"]
        if sbom.name != f"sbom-{arch}.cdx.json" or sbom.parent != directory / arch or digest(sbom.read_bytes()) != record["sbom"]["sha256"]:
            raise ValueError("SBOM does not match qualification")
        records.append(record)
    if records[0]["inputs"] != records[1]["inputs"]:
        raise ValueError("architecture builds used different inputs")
    return records


def assemble():
    sha, version = source_identity()
    records = qualification(OUT, sha, version)
    refs = [r["image"] for r in records]
    tag = f"{env('CI_REGISTRY_IMAGE')}:candidate-{sha}-{env('CI_PIPELINE_ID')}"
    run("docker", "buildx", "imagetools", "create", "--tag", tag, *refs)
    raw = Registry().raw(tag)
    index = json.loads(raw)
    actual = {(m["platform"]["architecture"], m["digest"]) for m in index["manifests"] if m["platform"]["os"] == "linux"}
    if actual != {(r["arch"], r["digest"]) for r in records} or len(index["manifests"]) != 2:
        raise ValueError("release index does not contain exactly the qualified architectures")
    write_json(OUT / "plan.json", {"schema": 1, "source": sha, "version": version,
        "image": env("CI_REGISTRY_IMAGE") + "@" + digest(raw), "digest": digest(raw)})
    public = provenance(records, sha, version, digest(raw))
    write_json(OUT / "provenance.json", public)
    assets = [OUT / "provenance.json"] + [OUT / a / f"sbom-{a}.cdx.json" for a in ARCHES]
    (OUT / "SHA256SUMS").write_text(checksums(assets))
    # Preserve private development aliases only after both architectures have
    # qualified. Public tags are exclusively the manual publication job's job.
    registry = Registry()
    repository = env("CI_REGISTRY_IMAGE")
    qualified = repository + "@" + digest(raw)
    registry.copy(qualified, repository + ":sha-" + sha[:8])
    if os.environ.get("CI_COMMIT_BRANCH") == os.environ.get("CI_DEFAULT_BRANCH", "main"):
        registry.copy(qualified, repository + ":edge")


def provenance(records, sha, version, index_digest):
    return {"schema": 1, "source": {"repository": "https://github.com/maxfield-allison/dnsweaver", "commit": sha},
        "version": version, "index_digest": index_digest, "signing": records[0]["inputs"]["signing"],
        "provenance_kind": "declared build and qualification record; not a signed attestation",
        "architectures": [{k: r[k] for k in ("arch", "digest", "inputs", "runtime", "scan", "sbom")} for r in records]}


def checksums(assets):
    return "".join(f"{hashlib.sha256(p.read_bytes()).hexdigest()}  {p.name}\n" for p in assets)


class GitHub:
    def __init__(self, repository, token):
        if not re.fullmatch(r"[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+", repository):
            raise ValueError("invalid GitHub repository")
        self.base = "https://api.github.com/repos/" + repository
        self.token = token

    def request(self, path, method="GET", data=None, binary=False):
        url = path if path.startswith("https://") else self.base + path
        if urllib.parse.urlparse(url).hostname not in ("api.github.com", "uploads.github.com"):
            raise ValueError("unexpected GitHub API host")
        body = data if binary else (json.dumps(data).encode() if data is not None else None)
        request = urllib.request.Request(url, data=body, method=method, headers={
            "Authorization": "Bearer " + self.token, "Accept": "application/vnd.github+json",
            "Content-Type": "application/octet-stream" if binary else "application/json", "User-Agent": "dnsweaver-release"})
        # Authenticated API requests must not forward credentials on redirects.
        class NoRedirect(urllib.request.HTTPRedirectHandler):
            def redirect_request(self, req, fp, code, msg, headers, newurl):
                return None
        with urllib.request.build_opener(NoRedirect).open(request, timeout=60) as response:
            return json.load(response)

    def tag_commit(self, version):
        obj = self.request("/git/ref/tags/" + version)["object"]
        for _ in range(4):
            if obj["type"] == "commit":
                return obj["sha"]
            if obj["type"] != "tag":
                break
            obj = self.request("/git/tags/" + obj["sha"])["object"]
        raise ValueError("public release tag does not resolve to a commit")

    def prepare(self, plan, body, assets):
        try:
            release = self.request("/releases/tags/" + plan["version"])
        except urllib.error.HTTPError as error:
            if error.code != 404:
                raise
            # The tag endpoint may omit drafts; enumerate before creating one
            # so a retry after an upload failure resumes the existing draft.
            release = None
            page = 1
            while release is None:
                batch = self.request(f"/releases?per_page=100&page={page}")
                release = next((r for r in batch if r["tag_name"] == plan["version"]), None)
                if len(batch) < 100:
                    break
                page += 1
            if release is None:
                release = self.request("/releases", "POST", {"tag_name": plan["version"], "target_commitish": plan["source"],
                    "name": plan["version"], "body": body, "draft": True, "prerelease": "-" in plan["version"], "make_latest": "false"})
        if release["body"] != body or release["prerelease"] != ("-" in plan["version"]):
            raise ValueError("existing release has different notes or prerelease status")
        existing = {a["name"]: a for a in self.request(f"/releases/{release['id']}/assets?per_page=100")}
        for asset in assets:
            wanted = digest(asset.read_bytes())
            if asset.name in existing:
                if existing[asset.name].get("digest") != wanted:
                    raise ValueError("existing release asset has a different or unavailable digest")
                continue
            if not release["draft"]:
                raise ValueError("published release is missing qualification evidence")
            upload = release["upload_url"].split("{")[0] + "?" + urllib.parse.urlencode({"name": asset.name})
            receipt = self.request(upload, "POST", asset.read_bytes(), binary=True)
            if receipt.get("digest") != wanted:
                raise ValueError("uploaded evidence digest mismatch")
        return release

    def publish(self, release):
        if release["draft"]:
            release = self.request(f"/releases/{release['id']}", "PATCH", {"draft": False, "make_latest": "false"})
        if release["draft"]:
            raise RuntimeError("release is still draft")

    def latest(self):
        return self.request("/releases/latest")["id"]

    def set_latest(self, release_id):
        self.request(f"/releases/{release_id}", "PATCH", {"make_latest": "true"})
        if self.latest() != release_id:
            raise RuntimeError("GitHub latest release did not update")


def promote(plan, registries, github, registry, notes, assets, journal):
    if github.tag_commit(plan["version"]) != plan["source"]:
        raise ValueError("public tag does not match qualified source")
    if registry.resolve(plan["image"]) != plan["digest"]:
        raise ValueError("qualified index digest mismatch")
    # Existing latest is required for rollback. First publication/bootstrap is
    # deliberately a separate operator action, not an implicit unsafe fallback.
    stable = "-" not in plan["version"]
    previous = {r: registry.resolve(r + ":latest") for r in registries} if stable else {}
    previous_release = github.latest() if stable else None
    state = {"source": plan["source"], "version": plan["version"], "previous_latest": previous, "previous_release": previous_release}
    write_json(journal, {**state, "phase": "prepared"})
    for repository in registries:
        registry.immutable_copy(plan["image"], repository + ":" + plan["version"])
    release = github.prepare(plan, notes, assets)
    github.publish(release)
    write_json(journal, {**state, "phase": "release-published"})
    changed = []
    release_changed = False
    try:
        for repository in previous:
            changed.append(repository)  # include an ambiguous failed write
            registry.copy(repository + ":" + plan["version"], repository + ":latest")
        if stable:
            release_changed = True
            github.set_latest(release["id"])
    except Exception:
        failures = []
        if release_changed:
            try:
                github.set_latest(previous_release)
            except Exception:
                failures.append("github-latest")
        for repository in reversed(changed):
            try:
                registry.copy(repository + "@" + previous[repository], repository + ":latest")
            except Exception:
                failures.append(repository)
        write_json(journal, {**state, "phase": "latest-update-failed", "rollback_failures": failures})
        raise
    write_json(journal, {**state, "phase": "complete"})


def publish():
    if env("RELEASE_PUBLISH") != "yes":
        raise ValueError("publication requires RELEASE_PUBLISH=yes")
    plan = read_json(OUT / "plan.json")
    sha, version = source_identity()
    if plan["source"] != sha or plan["version"] != version or not VERSION.fullmatch(version):
        raise ValueError("publication requires the qualified release tag")
    records = qualification(OUT, sha, version)
    if not DIGEST.fullmatch(plan["digest"]) or not plan["image"].endswith("@" + plan["digest"]):
        raise ValueError("release plan is not digest-bound")
    if read_json(OUT / "provenance.json") != provenance(records, sha, version, plan["digest"]):
        raise ValueError("provenance differs from qualification")
    evidence = [OUT / "provenance.json"] + [OUT / a / f"sbom-{a}.cdx.json" for a in ARCHES]
    if (OUT / "SHA256SUMS").read_text() != checksums(evidence):
        raise ValueError("release checksums differ from qualification")
    changelog = (ROOT / "CHANGELOG.md").read_text()
    match = re.search(r"^## \[" + re.escape(version[1:]) + r"\][^\n]*\n(.*?)(?=^## \[|\Z)", changelog, re.M | re.S)
    if not match or not match[1].strip():
        raise ValueError("release has no versioned changelog entry")
    repositories = ["ghcr.io/" + env("GITHUB_REPO"), "docker.io/" + env("DOCKERHUB_USERNAME") + "/dnsweaver"]
    notes = match[1].strip() + "\n\n## Container images\n\n" + "\n".join(f"- `{r}:{version}`" for r in repositories)
    notes += "\n\nImage index: `" + plan["digest"] + "`. Qualification evidence and SBOMs are attached.\n"
    assets = [OUT / "provenance.json", OUT / "SHA256SUMS"] + [OUT / a / f"sbom-{a}.cdx.json" for a in ARCHES]
    # The staging registry retains the same stable aliases for private users;
    # its name belongs only in the private plan/journal, never public notes.
    promote(plan, repositories + [env("CI_REGISTRY_IMAGE")], GitHub(env("GITHUB_REPO"), env("GITHUB_TOKEN")), Registry(), notes, assets, OUT / "publication.json")


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("command", choices=("build", "scan", "assemble", "publish"))
    parser.add_argument("--arch", choices=ARCHES)
    args = parser.parse_args()
    if args.command in ("build", "scan"):
        if not args.arch:
            parser.error("--arch is required")
        {"build": build, "scan": scan}[args.command](args.arch)
    else:
        {"assemble": assemble, "publish": publish}[args.command]()


if __name__ == "__main__":
    main()
