# Model Pricing Data

This directory contains a local copy of the mirrored model pricing data as a fallback mechanism.

## Source
The original file is maintained by the LiteLLM project and mirrored into the `price-mirror` branch of this repository via GitHub Actions:
- Mirror branch (configurable via `PRICE_MIRROR_REPO`): https://raw.githubusercontent.com/<your-repo>/price-mirror/model_prices_and_context_window.json
- Upstream source: https://raw.githubusercontent.com/BerriAI/litellm/main/model_prices_and_context_window.json

## Purpose
This local copy serves as a fallback when the remote file cannot be downloaded due to:
- Network restrictions
- Firewall rules
- DNS resolution issues
- GitHub being blocked in certain regions
- Docker container network limitations

## Update Process
The pricingService will:
1. First attempt to download the latest version from GitHub
2. If download fails, use this local copy as fallback
3. Log a warning when using the fallback file

## Manual Update
To manually update this file with the latest pricing data (if automation is unavailable):
```bash
curl -s https://raw.githubusercontent.com/BerriAI/litellm/main/model_prices_and_context_window.json -o model_prices_and_context_window.json
```

## File Format
The file contains JSON data with model pricing information including:
- Model names and identifiers
- Input/output token costs
- Context window sizes
- Model capabilities

Last updated: 2025-08-10

## GPT-6.1 Sol

The `gpt-6.1-sol` entry was verified against the [official model page](https://developers.openai.com/api/docs/models/gpt-6.1-sol) and [pricing page](https://developers.openai.com/api/docs/pricing) on 2026-09-30. Standard USD rates per million tokens are $2 input, $0.10 cached input, $2.50 cache writes, and $10 output. Fast is 2x; Batch and Flex are 0.5x. Above 272,000 input tokens, input/cache rates are 2x and output is 1.5x for the full request.

Context is 1,050,000 tokens with 128,000 maximum output tokens. Reasoning supports `low`, `medium` (default), `high`, `xhigh`, and `max`; `none` and `minimal` are unsupported. Tool calling uses Responses; upstream Chat Completions supports this model without tool calling.
