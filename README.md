# tasklease

`tasklease`는 Git 저장소 상태를 포함한 작업 인계용 JSON 엔벨로프를 만들고, 검증하고, 비교하는 Go CLI입니다.

## 기능

- `compile`: 현재 Git 상태와 플래그를 바탕으로 작업 인계 파일을 생성합니다.
- `validate`: 생성된 엔벨로프가 현재 Git 상태와 맞는지 검사합니다.
- `diff`: 두 엔벨로프의 차이를 안정적인 텍스트 형식으로 출력합니다.

## 요구 사항

- Go 1.22 이상
- Git

## 설치

저장소를 클론한 뒤 바로 빌드할 수 있습니다.

```bash
git clone https://github.com/ratelworks/tasklease.git
cd tasklease
go test ./...
go vet ./...
go build -o tasklease .
```

로컬에 설치하려면 저장소 루트에서 다음을 실행합니다.

```bash
go install .
```

설치 후에는 `tasklease --help`로 사용 가능한 명령을 확인할 수 있습니다.

## 사용법

### 1. 엔벨로프 생성

`compile`은 현재 Git 저장소의 `HEAD`, 작업 트리 상태, 브랜치, 접두 경로를 읽어 JSON을 만듭니다.

```bash
tasklease compile \
  --task "internal/tasklease의 검증 규칙 정리" \
  --name tasklease-cleanup \
  --tool git \
  --tool shell \
  --tool go \
  --artifact reports/tasklease.md \
  --budget-minutes 120 \
  --budget-files 10 \
  --repo . \
  --output lease.json
```

출력을 파일로 저장하지 않으면 표준 출력으로 JSON이 출력됩니다.

```bash
tasklease compile \
  --task "README 보강" \
  --tool git \
  --tool shell \
  --artifact notes/hand-off.md \
  --repo .
```

### 2. 엔벨로프 검증

`validate`는 저장된 엔벨로프가 현재 Git `HEAD`와 일치하는지, 작업 트리가 깨끗한지, 도구와 경로가 허용 규칙을 따르는지 확인합니다.

```bash
tasklease validate --repo . lease.json
```

검증 결과는 다음과 같은 형식으로 출력됩니다.

```text
Git: OK
Issue: git HEAD matches the resume checkpoint and the tree is clean.

Tools: OK
Issue: tool subset is explicit and supported.

Handoff: WARN
Issue: no artifact paths are declared.
Fix: Add at least one output path so the handoff tells the next agent where to write results.
```

### 3. 엔벨로프 비교

`diff`는 두 JSON 파일의 필드 차이를 읽기 쉬운 텍스트로 보여줍니다.

```bash
tasklease diff lease-old.json lease-new.json
```

예시 출력:

```text
toolSubset
Left:  [git, shell]
Right: [git, shell, go]

repo.revision
Left:  abc123
Right: def456
```

## 지원 도구

`compile`과 `validate`에서 허용하는 도구 값은 다음과 같습니다.

- `git`
- `shell`
- `go`
- `make`
- `fs`
- `test`

## 개발

로컬에서 변경 후에는 아래 명령으로 빠르게 확인할 수 있습니다.

```bash
go test ./...
go vet ./...
```

## 라이선스

MIT License. 자세한 내용은 [`LICENSE`](LICENSE)를 참고하세요.
