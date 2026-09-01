# VirusTotal API Key Environment Variables Nomenclature

When working with VirusTotal's official tools and SDKs, the nomenclature for the API key environment variable varies depending on the specific tool. There is no single globally standardized environment variable for all of VirusTotal's ecosystem, but there are distinct official conventions for their tools.

## Official VirusTotal CLI (`vt-cli`)

The official VirusTotal Command Line Interface (`vt-cli`) natively recognizes and uses the following environment variable:

- **`VTCLI_APIKEY`**

### Precedence in `vt-cli`
According to the official [vt-cli GitHub Repository](https://github.com/VirusTotal/vt-cli), the tool resolves the API key in the following order:
1. The `--apikey` command-line flag.
2. The **`VTCLI_APIKEY`** environment variable.
3. The configuration file (`~/.vt.toml`), generated via the `vt init` command.

## Official SDKs (`vt-py`, `vt-go`)

Unlike the CLI, the official VirusTotal SDKs for Python and Go **do not** automatically read from a predefined environment variable. They require the API key to be explicitly passed during client initialization.

### Python (`vt-py`)
The official [vt-py library](https://github.com/VirusTotal/vt-py) requires you to pass the key directly:
```python
import vt
import os

# Developers must manually implement the env var lookup
api_key = os.environ.get("VT_API_KEY")
client = vt.Client(api_key)
```

### Go (`vt-go`)
Similarly, the official [vt-go library](https://github.com/VirusTotal/vt-go) expects the key to be passed to its constructor:
```go
import (
    "os"
    "github.com/VirusTotal/vt-go"
)

// Developers manually fetch the key
apiKey := os.Getenv("VT_API_KEY")
client := vt.NewClient(apiKey)
```

While **`VT_API_KEY`** is frequently used in community tutorials, official examples, and best-practice guides for these SDKs, it is completely arbitrary and at the discretion of the developer implementing the code.

## Unofficial & Community Libraries

It is worth noting that some popular *unofficial* community libraries have implemented their own environment variables:
- **`virustotal-python`** (A popular unofficial Python library) automatically looks for **`VIRUSTOTAL_API_KEY`**.

## Conclusion

- If you are interacting with the **official CLI tool**, use **`VTCLI_APIKEY`**.
- If you are building with the **official SDKs**, the environment variable is not enforced by the SDK. Using **`VT_API_KEY`** is the general developer convention, but you must write the code to fetch it and pass it to the client manually.
