# Orthogonal Configuration Tests

This directory contains comprehensive tests for validating complex configuration combinations in the New API system.

## Test Series

### OC Series - Channel Multi-Group Configuration Tests
- **File**: `channel_multi_group_test.go`
- **Focus**: Channels authorized to multiple P2P groups
- **Test Cases**: OC-01 through OC-07
- **Key Scenarios**:
  - Multiple P2P group authorization
  - User matching multiple authorized groups
  - Cross-system-group + P2P authorization
  - Private channel multi-group authorization (should be invalid)
  - Channel statistics aggregation across groups

### OT Series - User Multi-Token Configuration Tests
- **File**: `user_multi_token_test.go`
- **Focus**: Same user with different token configurations
- **Test Cases**: OT-01 through OT-07
- **Key Scenarios**:
  - Different billing groups per token
  - Token billing group list fallback
  - Different P2P group restrictions per token
  - Token model limits + group combination
  - Token quota independent statistics
  - Concurrent requests from multiple tokens

### OM Series - Orthogonal Matrix Tests
- **File**: `orthogonal_matrix_test.go`
- **Focus**: Systematic coverage of 7-factor combinations using L18 orthogonal array
- **Test Cases**: OM-01 through OM-12
- **Key Factors**:
  - Channel: System Group (default/vip/svip), P2P Auth, Privacy
  - User: System Group, P2P Membership
  - Token: Billing Groups, P2P Restrictions
- **Purpose**: Verify routing logic with minimal test cases while maximizing coverage

### OS Series - Configuration Combination Statistics Tests
- **File**: `config_stats_test.go`
- **Focus**: Statistical correctness in complex configuration scenarios
- **Test Cases**: OS-01 through OS-04
- **Key Scenarios**:
  - Multi-token same-user statistics
  - Multi-group channel statistics aggregation
  - Token billing group switching statistics
  - Multi-model multi-group statistics

## Design Principles

1. **Separation of Concerns**: Each series focuses on a specific dimension
2. **Minimal Redundancy**: Orthogonal design reduces test case explosion
3. **Data Isolation**: Each test uses independent fixtures
4. **Comprehensive Coverage**: 7 factors × 3-4 levels = 3,456 combinations → 30 tests

## Core Routing Logic Validated

```
BillingGroupList = Token.Group OR User.Group
EffectiveP2P = Token.p2p_group_id (if set) OR User's all P2P groups
RoutingGroups = {BillingGroup} ∪ {EffectiveP2P}

For each BillingGroup in BillingGroupList (ordered):
    Find channels where:
        - Channel.SystemGroup ∈ BillingGroup
        - AND (Channel.P2PAuth ∩ EffectiveP2P ≠ ∅ OR Channel has no P2P)
        - AND (Channel.IsPrivate = false OR User = Channel.Owner)
    If found: Use this BillingGroup for billing, stop
    Else: Continue to next BillingGroup
```

## Running Tests

```bash
# Run all orthogonal tests
cd scene_test/new-api-monitoring-stats/orthogonal-config
go test -v

# Run specific series
go test -v -run TestOC  # OC series only
go test -v -run TestOT  # OT series only
go test -v -run TestOM  # OM series only
go test -v -run TestOS  # OS series only

# Run specific test case
go test -v -run TestOC01_ChannelMultiGroupAuthorization
```

## Dependencies

- **Fixtures**: `testutil/orthogonal_fixtures.go`
- **Helpers**: `testutil/group_helper.go`, `testutil/channel_stats_helper.go`
- **Mock Servers**: `testutil/mock_upstream.go`
