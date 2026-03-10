# rblnlib-go

Rebellions의 NPU 관리 라이브러러를 Go 언어로 추상화하여 제공하는 High Level Library입니다. 이 라이브러리를 사용하여 Type safe하게 NPU의 정보를 조회하고, 상태를 체크하며 NPU를 제어할 수 있습니다.

## Packages

| Package | Description |
|---------|-------------|
| rblnsmi | `rbln-smi` 명령어를 Go 언어로 추상화하여 제공합니다. |
| rsdgroup | 다수의 NPU를 하나의 RSD Group으로 그루핑하는 기능을 제공합니다 |
| device | NPU의 정보를 조회하고, 상태를 체크하며 하드웨어 정보를 제공합니다 |

## 주의사항

현재 NPU Device의 정보를 조회하기 위해서 위 모든 패키지들은 `rbln-smi` 바이너리에 직접적인 의존성을 가집니다. 따라서, 이 패키지가 실행되는 런타임 환경에서는 `rbln-smi` 바이너리가 반드시 설치되어 있어야하며 실행할 수 있어야만 합니다.

### 설치

```bash
go get github.com/rbln-sw/rblnlib-go
```
