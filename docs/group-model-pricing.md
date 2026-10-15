# Group-specific request prices

`billing_setting.group_model_price` is a JSON object mapping routing groups to
public model names and per-request base prices in USD. Save it through the root
admin `PUT /api/option/` endpoint, with the JSON encoded as the `value` string.
Updates replace the complete object; preserve unrelated entries when editing.

For example:

```json
{
  "gemini-ultra-per-call": {
    "gemini-4-argon": 1
  }
}
```

A matching entry overrides global token or expression billing for that exact
group and public model. The user's effective group multiplier still applies:
a base price of $1 and multiplier 0.07 charges $0.07 per request. A zero price
explicitly selects free per-request billing. Removing an entry restores the
model's global billing in that group. Empty names, null prices, negative prices,
and non-finite numbers are rejected. Normal quota bounds apply before sending
the upstream request.

Other groups use the existing global model prices, ratios, or expressions.
Channel model mappings remain independent: both groups can expose
`gemini-4-argon` and map it to the upstream `gemini-4-argon-high`.

Routing retries resolve billing against the selected group and reserve any
higher amount before sending another upstream request. Existing reservations
remain until settlement or refund. Switching through a fixed-price group does
not discard an already captured expression contract.

The pricing API returns separate rows with the same `model_name` for distinct
prices and their applicable `enable_groups`. Consumers must identify a pricing
row by model, billing type, and group set rather than model name alone. The
model list and metadata continue to use the single public model name.

This setting is currently managed through the options API; the model pricing
editor continues to manage the global fallback prices.
