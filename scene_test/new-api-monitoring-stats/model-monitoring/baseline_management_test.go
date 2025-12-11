package model_monitoring_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/suite"

	"github.com/QuantumNous/new-api/scene_test/testutil"
)

// BaselineManagementSuite tests the model baseline management functionality.
type BaselineManagementSuite struct {
	suite.Suite
	server   *testutil.TestServer
	client   *testutil.APIClient
	upstream *testutil.MockUpstreamServer
	fixtures *testutil.TestFixtures
}

// SetupSuite runs once before all tests in the suite.
func (s *BaselineManagementSuite) SetupSuite() {
	var err error
	s.server, err = testutil.StartTestServer()
	if err != nil {
		s.T().Fatalf("Failed to start test server: %v", err)
	}

	s.client = testutil.NewAPIClient(s.server)
	s.upstream = testutil.NewMockUpstreamServer()

	// Setup basic fixtures
	s.fixtures = testutil.NewTestFixtures(s.T(), s.client)
	s.fixtures.SetUpstream(s.upstream)

	// Create basic users and channels
	if err := s.fixtures.SetupBasicUsers(); err != nil {
		s.T().Fatalf("Failed to setup basic users: %v", err)
	}

	if err := s.fixtures.SetupBasicChannels(); err != nil {
		s.T().Fatalf("Failed to setup basic channels: %v", err)
	}
}

// TearDownSuite runs once after all tests in the suite.
func (s *BaselineManagementSuite) TearDownSuite() {
	if s.fixtures != nil {
		s.fixtures.Cleanup()
	}
	if s.upstream != nil {
		s.upstream.Close()
	}
	if s.server != nil {
		s.server.Stop()
	}
}

// SetupTest runs before each test.
func (s *BaselineManagementSuite) SetupTest() {
	// Reset upstream server state
	if s.upstream != nil {
		s.upstream.Reset()
	}
}

// TearDownTest runs after each test.
func (s *BaselineManagementSuite) TearDownTest() {
	// Clean up any baselines created during the test
	baselines, err := s.client.GetAllBaselines()
	if err == nil {
		for _, baseline := range baselines {
			s.client.DeleteBaseline(baseline.ID)
		}
	}
}

// TestMB01_CreateBaseline tests creating a new model baseline.
//
// Test ID: MB-01
// Priority: P0
// Test Scenario: 创建基准
// Expected Result: model_baselines 表新增记录, 返回201
func (s *BaselineManagementSuite) TestMB01_CreateBaseline() {
	s.T().Log("MB-01: Testing baseline creation")

	// Arrange: Prepare baseline data
	baseline := &testutil.ModelBaselineModel{
		ModelName:          "gpt-4",
		TestType:           "style",
		EvaluationStandard: "standard",
		BaselineChannelID:  s.fixtures.PublicChannel.ID,
		Prompt:             "请用简洁的语言解释什么是量子计算。",
		BaselineOutput:     "量子计算是一种利用量子力学原理进行信息处理的计算方式，通过量子比特的叠加和纠缠特性，可以在某些特定问题上实现比经典计算机更高的计算效率。",
	}

	// Act: Create the baseline
	baselineID, err := s.client.CreateBaseline(baseline)

	// Assert: Verify creation success
	assert.NoError(s.T(), err, "Baseline creation should succeed")
	assert.Greater(s.T(), baselineID, 0, "Baseline ID should be positive")

	// Verify the baseline can be retrieved
	retrieved, err := s.client.GetBaseline(baseline.ModelName, baseline.TestType, baseline.EvaluationStandard)
	assert.NoError(s.T(), err, "Should be able to retrieve the created baseline")
	assert.NotNil(s.T(), retrieved, "Retrieved baseline should not be nil")
	assert.Equal(s.T(), baseline.ModelName, retrieved.ModelName)
	assert.Equal(s.T(), baseline.TestType, retrieved.TestType)
	assert.Equal(s.T(), baseline.EvaluationStandard, retrieved.EvaluationStandard)
	assert.Equal(s.T(), baseline.Prompt, retrieved.Prompt)
	assert.Equal(s.T(), baseline.BaselineOutput, retrieved.BaselineOutput)

	s.T().Logf("MB-01: Successfully created baseline with ID %d", baselineID)
}

// TestMB02_BaselineUniquenessConstraint tests the unique constraint on (model_name, test_type, evaluation_standard).
//
// Test ID: MB-02
// Priority: P0
// Test Scenario: 基准唯一性约束
// Expected Result: 第二次请求更新现有记录，而非新增
func (s *BaselineManagementSuite) TestMB02_BaselineUniquenessConstraint() {
	s.T().Log("MB-02: Testing baseline uniqueness constraint")

	// Arrange: Create the first baseline
	baseline1 := &testutil.ModelBaselineModel{
		ModelName:          "gpt-4",
		TestType:           "style",
		EvaluationStandard: "standard",
		BaselineChannelID:  s.fixtures.PublicChannel.ID,
		Prompt:             "测试Prompt 1",
		BaselineOutput:     "测试输出 1",
	}

	id1, err := s.client.CreateBaseline(baseline1)
	assert.NoError(s.T(), err, "First baseline creation should succeed")
	s.T().Logf("Created first baseline with ID: %d", id1)

	// Act: Try to create a second baseline with the same (model_name, test_type, evaluation_standard)
	baseline2 := &testutil.ModelBaselineModel{
		ModelName:          "gpt-4",
		TestType:           "style",
		EvaluationStandard: "standard",
		BaselineChannelID:  s.fixtures.PublicChannel.ID,
		Prompt:             "测试Prompt 2 (不同内容)",
		BaselineOutput:     "测试输出 2 (不同内容)",
	}

	id2, err := s.client.CreateBaseline(baseline2)
	assert.NoError(s.T(), err, "Second baseline creation should succeed (update)")

	// Assert: Verify that only one baseline exists and it was updated
	retrieved, err := s.client.GetBaseline(baseline1.ModelName, baseline1.TestType, baseline1.EvaluationStandard)
	assert.NoError(s.T(), err, "Should be able to retrieve the baseline")
	assert.NotNil(s.T(), retrieved, "Retrieved baseline should not be nil")

	// The baseline should have been updated with new content
	assert.Equal(s.T(), baseline2.Prompt, retrieved.Prompt, "Prompt should be updated")
	assert.Equal(s.T(), baseline2.BaselineOutput, retrieved.BaselineOutput, "Output should be updated")

	// Verify that we didn't create duplicate records
	allBaselines, err := s.client.GetAllBaselines()
	assert.NoError(s.T(), err, "Should be able to get all baselines")

	// Count baselines matching our criteria
	count := 0
	for _, b := range allBaselines {
		if b.ModelName == "gpt-4" && b.TestType == "style" && b.EvaluationStandard == "standard" {
			count++
		}
	}
	assert.Equal(s.T(), 1, count, "Should have exactly one baseline for this combination")

	s.T().Logf("MB-02: Uniqueness constraint verified - baseline was updated, not duplicated (ID: %d -> %d)", id1, id2)
}

// TestMB03_GetAllBaselines tests retrieving all baselines.
//
// Test ID: MB-03
// Priority: P1
// Test Scenario: 基准查询
// Expected Result: 返回所有已配置的基准列表
func (s *BaselineManagementSuite) TestMB03_GetAllBaselines() {
	s.T().Log("MB-03: Testing get all baselines")

	// Arrange: Create multiple baselines with different configurations
	baselines := []*testutil.ModelBaselineModel{
		{
			ModelName:          "gpt-4",
			TestType:           "style",
			EvaluationStandard: "standard",
			BaselineChannelID:  s.fixtures.PublicChannel.ID,
			Prompt:             "Style test prompt",
			BaselineOutput:     "Style test output",
		},
		{
			ModelName:          "gpt-4",
			TestType:           "reasoning",
			EvaluationStandard: "standard",
			BaselineChannelID:  s.fixtures.PublicChannel.ID,
			Prompt:             "Reasoning test prompt",
			BaselineOutput:     "Reasoning test output",
		},
		{
			ModelName:          "gpt-3.5-turbo",
			TestType:           "encoding",
			EvaluationStandard: "strict",
			BaselineChannelID:  s.fixtures.PublicChannel.ID,
			Prompt:             "Encoding test prompt",
			BaselineOutput:     "Encoding test output",
		},
	}

	createdIDs := make([]int, 0)
	for _, baseline := range baselines {
		id, err := s.client.CreateBaseline(baseline)
		assert.NoError(s.T(), err, "Baseline creation should succeed")
		createdIDs = append(createdIDs, id)
	}
	s.T().Logf("Created %d baselines", len(createdIDs))

	// Act: Retrieve all baselines
	allBaselines, err := s.client.GetAllBaselines()

	// Assert: Verify all baselines are returned
	assert.NoError(s.T(), err, "Get all baselines should succeed")
	assert.GreaterOrEqual(s.T(), len(allBaselines), len(baselines), "Should return at least the created baselines")

	// Verify each created baseline is in the list
	for i, baseline := range baselines {
		found := false
		for _, retrieved := range allBaselines {
			if retrieved.ModelName == baseline.ModelName &&
				retrieved.TestType == baseline.TestType &&
				retrieved.EvaluationStandard == baseline.EvaluationStandard {
				found = true
				assert.Equal(s.T(), baseline.Prompt, retrieved.Prompt)
				assert.Equal(s.T(), baseline.BaselineOutput, retrieved.BaselineOutput)
				break
			}
		}
		assert.True(s.T(), found, "Baseline %d should be found in the list", i)
	}

	s.T().Logf("MB-03: Successfully retrieved %d baselines", len(allBaselines))
}

// TestMB04_UpdateBaseline tests updating an existing baseline.
//
// Test ID: MB-04
// Priority: P1
// Test Scenario: 基准更新
// Expected Result: 基准的 baseline_output 被更新
func (s *BaselineManagementSuite) TestMB04_UpdateBaseline() {
	s.T().Log("MB-04: Testing baseline update")

	// Arrange: Create an initial baseline
	originalBaseline := &testutil.ModelBaselineModel{
		ModelName:          "gpt-4",
		TestType:           "style",
		EvaluationStandard: "standard",
		BaselineChannelID:  s.fixtures.PublicChannel.ID,
		Prompt:             "Original prompt",
		BaselineOutput:     "Original output",
	}

	id, err := s.client.CreateBaseline(originalBaseline)
	assert.NoError(s.T(), err, "Initial baseline creation should succeed")
	s.T().Logf("Created baseline with ID: %d", id)

	// Wait a bit to ensure timestamp difference
	time.Sleep(100 * time.Millisecond)

	// Act: Update the baseline with a new channel and output
	updatedBaseline := &testutil.ModelBaselineModel{
		ModelName:          "gpt-4",
		TestType:           "style",
		EvaluationStandard: "standard",
		BaselineChannelID:  s.fixtures.GroupChannel1.ID, // Different channel
		Prompt:             "Updated prompt",
		BaselineOutput:     "Updated output - This is the new baseline content",
	}

	_, err = s.client.CreateBaseline(updatedBaseline) // Using Create for upsert behavior
	assert.NoError(s.T(), err, "Baseline update should succeed")

	// Assert: Verify the baseline was updated
	retrieved, err := s.client.GetBaseline(originalBaseline.ModelName, originalBaseline.TestType, originalBaseline.EvaluationStandard)
	assert.NoError(s.T(), err, "Should be able to retrieve the updated baseline")
	assert.NotNil(s.T(), retrieved, "Retrieved baseline should not be nil")

	// Verify updated fields
	assert.Equal(s.T(), updatedBaseline.BaselineChannelID, retrieved.BaselineChannelID, "Channel ID should be updated")
	assert.Equal(s.T(), updatedBaseline.Prompt, retrieved.Prompt, "Prompt should be updated")
	assert.Equal(s.T(), updatedBaseline.BaselineOutput, retrieved.BaselineOutput, "Output should be updated")

	// Verify no duplicate baselines were created
	allBaselines, err := s.client.GetAllBaselines()
	assert.NoError(s.T(), err, "Should be able to get all baselines")

	count := 0
	for _, b := range allBaselines {
		if b.ModelName == originalBaseline.ModelName &&
			b.TestType == originalBaseline.TestType &&
			b.EvaluationStandard == originalBaseline.EvaluationStandard {
			count++
		}
	}
	assert.Equal(s.T(), 1, count, "Should still have exactly one baseline")

	s.T().Logf("MB-04: Successfully updated baseline - output changed from '%s' to '%s'",
		originalBaseline.BaselineOutput, updatedBaseline.BaselineOutput)
}

// TestRunner for the baseline management test suite
func TestBaselineManagementSuite(t *testing.T) {
	suite.Run(t, new(BaselineManagementSuite))
}
