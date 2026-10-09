# Threat Model: gh-pt Source Build System

**Version**: 1.0  
**Date**: 2026-10-09  
**Status**: Active  
**Classification**: Security-Critical

---

## 1. Executive Summary

This document presents a comprehensive threat model for the gh-pt source build system, which uses AI agents to generate build scripts for compiling software from source. The system employs a two-container architecture:

1. **AI Container**: Where the AI agent operates (network enabled, standard permissions)
2. **Compile Container**: Hardened container where compilation occurs (no network, minimal capabilities, seccomp)

The threat model identifies critical attack vectors, validates security controls, and provides a prioritized mitigation roadmap.

**Risk Level**: HIGH (without mitigations) → MEDIUM (with full hardening)

---

## 2. System Architecture

```
┌─────────────────────────────────────────────────────────────────────┐
│                         HOST SYSTEM                                 │
│                                                                     │
│  ┌───────────────────────────────────────────────────────────────┐ │
│  │ Stage 1: AI CONTAINER (Working Environment)                   │ │
│  │                                                               │ │
│  │  Network: ENABLED (for AI research/development)              │ │
│  │  Capabilities: Standard user permissions                     │ │
│  │  Filesystem: Writable /workspace                             │ │
│  │  Tools: AI agent (Claude/GPT) + development tools           │ │
│  │  Output: body.sh, manifest.json                              │ │
│  │                                                               │ │
│  │  Trust Level: LOW (AI is untrusted)                          │ │
│  └───────────────────────────────────────────────────────────────┘ │
│                              │                                      │
│                              ▼ (validate body.sh via AST parser)   │
│  ┌───────────────────────────────────────────────────────────────┐ │
│  │ Stage 2: COMPILE CONTAINER (Hardened Execution)              │ │
│  │                                                               │ │
│  │  Network: DISABLED (--network=none)                          │ │
│  │  Capabilities: DROP ALL, add only DAC_OVERRIDE               │ │
│  │  Filesystem: Read-only except /build, /install               │ │
│  │  Seccomp: Restrict to build-related syscalls only            │ │
│  │  User: Non-root (UID 1000)                                   │ │
│  │  Input: body.sh + deps (pre-installed in stage 1)            │ │
│  │  Output: Compiled binaries in /install                       │ │
│  │                                                               │ │
│  │  Trust Level: ZERO (assumes compromise)                      │ │
│  └───────────────────────────────────────────────────────────────┘ │
│                              │                                      │
│                              ▼ (extract binaries)                  │
│  ┌───────────────────────────────────────────────────────────────┐ │
│  │ Stage 3: POST-PROCESSING (Host)                                │ │
│  │                                                               │ │
│  │  - Extract binaries from /install                            │ │
│  │  - Validate checksums/signatures                             │ │
│  │  - Install to target location                                │ │
│  │  - Update state.json                                         │ │
│  └───────────────────────────────────────────────────────────────┘ │
└─────────────────────────────────────────────────────────────────────┘
```

### 2.1 Trust Boundaries

| Boundary | Description | Protection Level |
|----------|-------------|------------------|
| Host → AI Container | AI is untrusted, isolated from host | Container isolation |
| AI Container → Compile Container | body.sh validated, deps pre-installed | AST validation + network isolation |
| Compile Container → Host | Only binaries extracted, validated | Read-only filesystem + seccomp |
| Compile Container → Network | No network access | --network=none |

### 2.2 Security Controls

| Control | Implementation | Purpose |
|---------|---------------|---------|
| Container Isolation | Separate containers for AI and compile | Prevent AI from directly accessing host |
| Network Isolation | --network=none on compile container | Prevent data exfiltration |
| Capability Dropping | --cap-drop=ALL, --cap-add=DAC_OVERRIDE | Minimize privilege |
| Filesystem Isolation | --read-only + writable /build, /install | Prevent filesystem tampering |
| Seccomp Profile | Restrict to build-related syscalls | Prevent kernel exploits |
| Non-root Execution | UID 1000 in compile container | Prevent privilege escalation |
| AST Validation | Parse body.sh, check against allowlist | Prevent code injection |
| Audit Logging | Log all operations | Detect anomalies |

---

## 3. Threat Actors

### 3.1 Primary Threat: Malicious AI Agent

**Description**: AI agent (Claude, GPT, or custom model) generates malicious build scripts

**Motivation**:
- Adversarial input from user
- Compromised AI model
- AI hallucination leading to insecure code
- Prompt injection attack

**Capabilities**:
- Generate arbitrary shell code in body.sh
- Modify manifest.json to declare malicious dependencies
- Attempt to escape container
- Attempt network communication
- Attempt privilege escalation

**Sophistication**: HIGH (AI can generate complex obfuscated code)

### 3.2 Secondary Threat: Compromised Dependencies

**Description**: Dependencies declared in manifest.json are malicious

**Motivation**:
- Supply chain attack
- Typosquatting (similar package names)
- Compromised package registry

**Capabilities**:
- Execute arbitrary code during installation
- Install backdoors
- Exfiltrate data via network (if available)

**Sophistication**: MEDIUM

### 3.3 Tertiary Threat: Container Escape

**Description**: Exploit vulnerabilities in container runtime

**Motivation**:
- Gain host access
- Persist malicious artifacts
- Access sensitive data

**Capabilities**:
- Exploit kernel vulnerabilities
- Exploit container runtime bugs (runc, containerd)
- Exploit misconfigured capabilities

**Sophistication**: VERY HIGH (requires advanced exploit development)

---

## 4. Assets

| Asset | Description | Sensitivity | Impact if Compromised |
|-------|-------------|-------------|----------------------|
| Host Filesystem | All files on host system | CRITICAL | Complete system compromise |
| Compiled Binaries | Output of build process | HIGH | Malware distribution |
| Source Code | Input repository | MEDIUM | Code theft, intellectual property loss |
| Credentials | API keys, tokens, secrets | CRITICAL | Unauthorized access to services |
| Network Access | Host network connectivity | HIGH | Lateral movement, data exfiltration |
| State File | state.json with installation metadata | MEDIUM | Installation tracking compromise |

---

## 5. Attack Trees

### 5.1 Attack Goal: Execute Arbitrary Code on Host

```
Execute Arbitrary Code on Host
├── OR
│   ├── Container Escape
│   │   ├── AND
│   │   │   ├── Find kernel vulnerability (CVE)
│   │   │   ├── Develop exploit
│   │   │   └── Execute exploit in container
│   │   └── OR
│   │       ├── Exploit container runtime bug (runc)
│   │       ├── Exploit misconfigured capabilities
│   │       └── Exploit seccomp bypass
│   │
│   ├── Filesystem Write
│   │   ├── AND
│   │   │   ├── Find writable path in compile container
│   │   │   ├── Write malicious file
│   │   │   └── Execute via host cron/systemd
│   │   └── OR
│   │       ├── Exploit bind mount misconfiguration
│   │       └── Exploit volume mount escape
│   │
│   ├── Network Exfiltration
│   │   ├── AND
│   │   │   ├── Bypass --network=none
│   │   │   ├── Establish outbound connection
│   │   │   └── Download/execute payload
│   │   └── OR
│   │       ├── Exploit DNS tunneling
│   │       └── Exploit container network misconfiguration
│   │
│   └── Privilege Escalation
│       ├── AND
│       │   ├── Gain root in compile container
│       │   ├── Exploit capability (CAP_SYS_ADMIN)
│       │   └── Escape to host
│       └── OR
│           ├── Exploit SUID binary
│           ├── Exploit sudo misconfiguration
│           └── Exploit kernel privilege escalation
```

**Likelihood**: LOW (with full hardening)  
**Impact**: CRITICAL  
**Mitigation Priority**: P0

### 5.2 Attack Goal: Inject Malicious Code into Build

```
Inject Malicious Code into Build
├── OR
│   ├── Malicious body.sh
│   │   ├── AND
│   │   │   ├── AI generates malicious code
│   │   │   ├── Bypass AST validation
│   │   │   └── Execute during compilation
│   │   └── OR
│   │       ├── Obfuscate commands (base64, eval)
│   │       ├── Use heredocs to bypass regex
│   │       ├── Exploit build tool (cmake/make)
│   │       └── Use shell expansions (variable indirection)
│   │
│   ├── Malicious Dependencies
│   │   ├── AND
│   │   │   ├── AI declares malicious package
│   │   │   ├── Package installs in stage 1 (AI container)
│   │   │   └── Package executes malicious code
│   │   └── OR
│   │       ├── Typosquatting (similar package name)
│   │       ├── Compromised package registry
│   │       └── Malicious post-install script
│   │
│   └── Build Tool Abuse
│       ├── AND
│       │   ├── Use cmake/make to execute arbitrary code
│       │   └── Bypass validation (code looks legitimate)
│       └── OR
│           ├── cmake -P malicious_script.cmake
│           ├── make CC="malicious_command"
│           └── Build system plugins/extensions
```

**Likelihood**: HIGH (without AST validation) → LOW (with AST validation)  
**Impact**: HIGH  
**Mitigation Priority**: P0

### 5.3 Attack Goal: Exfiltrate Sensitive Data

```
Exfiltrate Sensitive Data
├── OR
│   ├── Network Exfiltration
│   │   ├── AND
│   │   │   ├── Access sensitive data (credentials, keys)
│   │   │   ├── Bypass --network=none
│   │   │   └── Send data to attacker server
│   │   └── OR
│   │       ├── DNS tunneling
│   │       ├── HTTP/HTTPS to external server
│   │       └── Exploit container network misconfiguration
│   │
│   ├── Filesystem Persistence
│   │   ├── AND
│   │   │   ├── Write sensitive data to filesystem
│   │   │   ├── Data persists after container exits
│   │   │   └── Attacker retrieves data later
│   │   └── OR
│   │       ├── Write to shared volume
│   │       ├── Write to bind mount
│   │       └── Exploit container storage misconfiguration
│   │
│   └── Side-Channel Attack
│       ├── AND
│       │   ├── Observe timing/memory patterns
│       │   ├── Infer sensitive data
│       │   └── Exfiltrate via covert channel
│       └── OR
│           ├── Timing attack on crypto operations
│           ├── Memory dump analysis
│           └── CPU cache side-channel
```

**Likelihood**: LOW (with network isolation)  
**Impact**: CRITICAL  
**Mitigation Priority**: P1

### 5.4 Attack Goal: Persist Malicious Artifacts

```
Persist Malicious Artifacts
├── OR
│   ├── Compiled Binaries
│   │   ├── AND
│   │   │   ├── Inject malicious code into build
│   │   │   ├── Compile successfully
│   │   │   └── Install to target location
│   │   └── OR
│   │       ├── Backdoor binary
│   │       ├── Replace legitimate binary
│   │       └── Install additional malware
│   │
│   ├── State File Manipulation
│   │   ├── AND
│   │   │   ├── Modify state.json
│   │   │   ├── Track malicious installation
│   │   │   └── Prevent removal
│   │   └── OR
│   │       ├── Add fake installation
│   │       ├── Modify paths to point to malware
│   │       └── Disable uninstallation
│   │
│   └── Filesystem Artifacts
│       ├── AND
│       │   ├── Write files to persistent storage
│       │   ├── Files survive container restart
│       │   └── Files execute on next boot/login
│       └── OR
│           ├── Cron job
│           ├── Systemd service
│           ├── Shell profile (~/.bashrc)
│           └── Startup script
```

**Likelihood**: MEDIUM (without post-processing validation)  
**Impact**: HIGH  
**Mitigation Priority**: P1

---

## 6. Vulnerability Analysis

### 6.1 Current Implementation Vulnerabilities

| ID | Vulnerability | Severity | Likelihood | Risk | Status |
|----|---------------|----------|------------|------|--------|
| V-001 | Process tree detection fails in containers | CRITICAL | HIGH | CRITICAL | Open |
| V-002 | Regex validation trivially bypassable | CRITICAL | HIGH | CRITICAL | Open |
| V-003 | Container lacks capability dropping | HIGH | MEDIUM | HIGH | Open |
| V-004 | Container lacks seccomp profile | HIGH | MEDIUM | HIGH | Open |
| V-005 | Container has network access | CRITICAL | HIGH | CRITICAL | Open |
| V-006 | Filesystem not read-only | HIGH | MEDIUM | HIGH | Open |
| V-007 | AI can create arbitrary artifacts | MEDIUM | HIGH | HIGH | Open |
| V-008 | No AST-based validation | CRITICAL | HIGH | CRITICAL | Open |
| V-009 | manifest.json not validated | HIGH | MEDIUM | HIGH | Open |
| V-010 | Compile.sh written to disk | MEDIUM | LOW | MEDIUM | Open |

### 6.2 Residual Risks (After Mitigations)

| ID | Risk | Severity | Likelihood | Risk | Mitigation |
|----|------|----------|------------|------|------------|
| R-001 | Kernel vulnerability in container | CRITICAL | LOW | MEDIUM | Regular updates, security patches |
| R-002 | Container runtime exploit (runc) | CRITICAL | LOW | MEDIUM | Keep runtime updated, monitor CVEs |
| R-003 | AI generates sophisticated obfuscated code | HIGH | MEDIUM | HIGH | AST validation + runtime monitoring |
| R-004 | Supply chain attack on dependencies | HIGH | MEDIUM | HIGH | Dependency validation, checksums |
| R-005 | Side-channel attacks | MEDIUM | LOW | LOW | Isolation, monitoring |

---

## 7. Mitigation Roadmap

### 7.1 Immediate (Week 1-2) - P0

#### 7.1.1 Container Hardening

**Goal**: Harden compile container to prevent escape and limit damage

**Implementation**:
```bash
docker run \
  --name compile-container \
  --network=none \
  --cap-drop=ALL \
  --cap-add=DAC_OVERRIDE \
  --security-opt=no-new-privileges \
  --security-opt seccomp=seccomp-profile.json \
  --read-only \
  --tmpfs /tmp:size=100M,mode=1777 \
  --tmpfs /build:size=1G,mode=1777 \
  --tmpfs /install:size=1G,mode=1777 \
  --user 1000:1000 \
  -v /path/to/source:/build:ro \
  -v /path/to/output:/install:rw \
  compile-image
```

**Validation**:
- [ ] Network disabled (verify with `ip addr`, `ping`)
- [ ] Capabilities dropped (verify with `capsh --print`)
- [ ] Filesystem read-only (verify with `mount | grep readonly`)
- [ ] Seccomp active (verify with `grep Seccomp /proc/1/status`)
- [ ] Non-root user (verify with `id`)

**Files**:
- `compile/container.go`: Update container execution logic
- `compile/seccomp-profile.json`: Seccomp profile
- `docs/SPEC-SOURCE-REPOS.md`: Document security model

---

#### 7.1.2 AST-Based Validation

**Goal**: Replace regex validation with AST-based bash parser

**Implementation**:
1. Use `mvdan.cc/sh` (Go bash parser) or `tree-sitter-bash`
2. Parse body.sh into AST
3. Walk AST, check against allowlist:
   - Allowed: build commands (cmake, make, gcc, etc.)
   - Allowed: `ghpt helper --install`
   - Forbidden: network commands (curl, wget, ssh)
   - Forbidden: privilege escalation (sudo, su)
   - Forbidden: shell expansions (eval, exec)
   - Forbidden: obfuscation (base64, hex escapes)

**Validation**:
- [ ] Parser correctly identifies all commands
- [ ] Allowlist blocks forbidden patterns
- [ ] Obfuscated code detected and blocked
- [ ] Build tools allowed but monitored

**Files**:
- `ai/validator.go`: AST-based validation
- `ai/validator_test.go`: Test cases
- `go.mod`: Add `mvdan.cc/sh` dependency

---

#### 7.1.3 Separate AI and Compile Containers

**Goal**: Isolate AI agent from compilation environment

**Implementation**:
```
Stage 1: AI Container
- Network: ENABLED
- Filesystem: Writable /workspace
- Tools: AI agent, git, curl, etc.
- Output: body.sh, manifest.json

Stage 2: Compile Container
- Network: DISABLED
- Filesystem: Read-only except /build, /install
- Tools: Build tools only (gcc, cmake, make)
- Input: body.sh, manifest.json, deps
- Output: Compiled binaries
```

**Validation**:
- [ ] AI container has network access
- [ ] Compile container has NO network access
- [ ] body.sh transferred securely between containers
- [ ] Dependencies installed in compile container

**Files**:
- `cmd/root.go`: Update workflow to use two containers
- `compile/container.go`: Add AI container logic
- `docs/SPEC-SOURCE-REPOS.md`: Document two-stage architecture

---

### 7.2 Short-Term (Month 1) - P1

#### 7.2.1 Manifest.json Validation

**Goal**: Validate dependencies declared in manifest.json

**Implementation**:
1. Parse manifest.json
2. Check each dependency against allowlist
3. Verify package names (no typosquatting)
4. Check package registry (official sources only)
5. Validate version constraints

**Validation**:
- [ ] Invalid packages rejected
- [ ] Typosquatting detected
- [ ] Unofficial registries blocked
- [ ] Version constraints enforced

**Files**:
- `ai/manifest_validator.go`: Manifest validation
- `ai/manifest_validator_test.go`: Test cases

---

#### 7.2.2 Execute from Memory

**Goal**: Never write compile.sh to disk

**Implementation**:
1. Read body.sh
2. Concatenate header + body.sh + footer (in memory)
3. Execute via pipe: `echo "$COMPILE_SCRIPT" | bash`
4. Or use `bash -c "$COMPILE_SCRIPT"`

**Validation**:
- [ ] compile.sh never written to disk
- [ ] Execution from memory works correctly
- [ ] Logs still captured

**Files**:
- `cmd/helper.go`: Update helperRunCompileScript()

---

#### 7.2.3 Audit All AI Artifacts

**Goal**: Track all files created by AI

**Implementation**:
1. Before build: Snapshot /build directory
2. During build: Monitor file creation (inotify)
3. After build: Compare snapshots
4. Log all new/modified files
5. Validate artifacts (checksums, signatures)

**Validation**:
- [ ] All AI-created files logged
- [ ] Unexpected files flagged
- [ ] Artifacts validated

**Files**:
- `compile/auditor.go`: File auditing
- `compile/auditor_test.go`: Test cases

---

### 7.3 Medium-Term (Month 2-3) - P2

#### 7.3.1 Runtime Monitoring

**Goal**: Monitor compile container for suspicious activity

**Implementation**:
1. Monitor syscalls (auditd or custom)
2. Monitor network attempts (should be blocked)
3. Monitor file access patterns
4. Alert on anomalies

**Validation**:
- [ ] Syscall monitoring active
- [ ] Network attempts logged
- [ ] Anomalies detected and alerted

**Files**:
- `compile/monitor.go`: Runtime monitoring
- `compile/monitor_test.go`: Test cases

---

#### 7.3.2 Dependency Isolation

**Goal**: Install dependencies in separate container

**Implementation**:
```
Stage 1: AI Container (network enabled)
Stage 2: Deps Container (network enabled, installs deps)
Stage 3: Compile Container (no network, uses pre-installed deps)
```

**Validation**:
- [ ] Dependencies installed in isolated container
- [ ] Compile container has no network
- [ ] Pre-installed deps available in compile container

**Files**:
- `cmd/root.go`: Update workflow to three stages
- `compile/container.go`: Add deps container logic

---

### 7.4 Long-Term (Month 4+) - P3

#### 7.4.1 Allowlist-Based Security Model

**Goal**: Replace blocklist with allowlist

**Implementation**:
1. Define allowed commands (cmake, make, gcc, etc.)
2. Define allowed syscalls (build-related only)
3. Define allowed file paths (/build, /install)
4. Block everything else

**Validation**:
- [ ] Only allowed commands execute
- [ ] Only allowed syscalls permitted
- [ ] Only allowed paths accessible

**Files**:
- `ai/validator.go`: Allowlist implementation
- `compile/seccomp-profile.json`: Allowlist syscalls

---

#### 7.4.2 Build Caching

**Goal**: Cache builds for speed and reproducibility

**Implementation**:
1. Hash body.sh + manifest.json + deps
2. Check cache for matching hash
3. If hit: use cached build
4. If miss: build and cache

**Validation**:
- [ ] Cache hits work correctly
- [ ] Cache misses build correctly
- [ ] Cache invalidated on changes

**Files**:
- `compile/cache.go`: Build caching
- `compile/cache_test.go`: Test cases

---

## 8. Testing Strategy

### 8.1 Security Tests

| Test ID | Test Case | Expected Result | Priority |
|---------|-----------|-----------------|----------|
| ST-001 | AI attempts network in compile container | Connection blocked | P0 |
| ST-002 | AI attempts sudo in body.sh | Validation fails | P0 |
| ST-003 | AI attempts to write outside /install | Permission denied | P0 |
| ST-004 | AI uses base64 obfuscation | Validation detects and blocks | P0 |
| ST-005 | AI uses heredoc to bypass validation | Validation detects and blocks | P0 |
| ST-006 | AI declares malicious dependency | Manifest validation rejects | P1 |
| ST-007 | AI attempts container escape | Escape fails | P1 |
| ST-008 | AI attempts privilege escalation | Escalation fails | P1 |

### 8.2 Integration Tests

| Test ID | Test Case | Expected Result | Priority |
|---------|-----------|-----------------|----------|
| IT-001 | Normal build succeeds | Build completes, binaries in /install | P0 |
| IT-002 | Build with deps succeeds | Deps installed, build completes | P0 |
| IT-003 | Build with network access blocked | Build fails gracefully | P0 |
| IT-004 | Build with invalid body.sh | Validation fails, build aborted | P0 |
| IT-005 | Build with invalid manifest | Validation fails, build aborted | P1 |

### 8.3 Fuzzing Tests

| Test ID | Test Case | Expected Result | Priority |
|---------|-----------|-----------------|----------|
| FT-001 | Fuzz body.sh with random commands | All invalid commands blocked | P1 |
| FT-002 | Fuzz manifest.json with random packages | All invalid packages rejected | P1 |
| FT-003 | Fuzz build tools with random flags | Build fails safely | P2 |

---

## 9. Incident Response Plan

### 9.1 Detection

**Indicators of Compromise (IoCs)**:
- Network connection attempts from compile container
- Syscalls outside allowlist
- File writes outside /build, /install
- Privilege escalation attempts
- Unusual build tool behavior

**Monitoring**:
- Container logs
- Audit logs
- Network logs (should be empty for compile container)
- Syscall logs

### 9.2 Containment

**Immediate Actions**:
1. Stop compile container: `docker stop compile-container`
2. Isolate host: Disable network if suspicious activity
3. Preserve evidence: Copy container filesystem, logs
4. Notify stakeholders: Security team, management

**Investigation**:
1. Analyze body.sh: What did AI generate?
2. Analyze manifest.json: What dependencies declared?
3. Analyze logs: What did compile container attempt?
4. Analyze artifacts: What files created?

### 9.3 Eradication

**Actions**:
1. Remove compromised artifacts
2. Rebuild from clean source
3. Update AI model (if compromised)
4. Patch vulnerabilities

### 9.4 Recovery

**Actions**:
1. Restore from backup (if needed)
2. Rebuild affected packages
3. Verify integrity of installed binaries
4. Monitor for recurrence

### 9.5 Lessons Learned

**Actions**:
1. Document incident
2. Update threat model
3. Improve security controls
4. Train team on lessons

---

## 10. Compliance and Standards

### 10.1 Relevant Standards

| Standard | Relevance | Compliance Status |
|----------|-----------|-------------------|
| OWASP Top 10 | Web application security | N/A (not web app) |
| CIS Docker Benchmark | Container security | Partial (see gaps below) |
| NIST SP 800-190 | Container security | Partial |
| MITRE ATT&CK | Attack techniques | Mapped (see attack trees) |

### 10.2 CIS Docker Benchmark Gaps

| Control | Status | Gap |
|---------|--------|-----|
| 5.1 AppArmor profile | Not implemented | Need AppArmor profile |
| 5.2 SELinux security options | Not implemented | Need SELinux policy |
| 5.4 Linux capabilities | Partial | Need to drop more caps |
| 5.5 Restrict Linux syscalls | Partial | Need seccomp profile |
| 5.7 Update container base image | Not applicable | Using ubuntu:22.04 |
| 5.10 Mount host filesystem read-only | Implemented | /build is read-only |
| 5.11 Set container user to non-root | Implemented | UID 1000 |

---

## 11. Appendices

### 11.1 Glossary

| Term | Definition |
|------|-----------|
| AST | Abstract Syntax Tree - parsed representation of code |
| Seccomp | Secure computing mode - syscall filtering |
| Capability | Linux privilege unit (e.g., CAP_SYS_ADMIN) |
| Bind mount | Mount host directory into container |
| Volume | Docker-managed storage |
| Attack tree | Hierarchical representation of attack paths |
| IoC | Indicator of Compromise |

### 11.2 References

- [Docker Security](https://docs.docker.com/engine/security/)
- [CIS Docker Benchmark](https://www.cisecurity.org/benchmark/docker)
- [NIST SP 800-190](https://csrc.nist.gov/publications/detail/sp/800-190/final)
- [MITRE ATT&CK](https://attack.mitre.org/)
- [OWASP Docker Security Cheat Sheet](https://cheatsheetseries.owasp.org/cheatsheets/Docker_Security_Cheat_Sheet.html)

### 11.3 Revision History

| Version | Date | Author | Changes |
|---------|------|--------|---------|
| 1.0 | 2026-10-09 | AI Assistant | Initial version |

---

## 12. Approval

| Role | Name | Signature | Date |
|------|------|-----------|------|
| Security Lead | | | |
| Engineering Lead | | | |
| Product Owner | | | |

---

**Document Status**: DRAFT - Pending Review  
**Next Review Date**: 2026-11-09  
**Review Frequency**: Monthly
