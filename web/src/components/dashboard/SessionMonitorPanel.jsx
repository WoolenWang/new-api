/*
Copyright (C) 2025 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/

import React, { useState, useEffect } from 'react';
import { useTranslation } from 'react-i18next';
import {
  Card,
  Typography,
  Table,
  Tag,
  Spin,
  Empty,
  Progress,
  Row,
  Col,
  Tooltip,
  Button,
  Space,
} from '@douyinfe/semi-ui';
import {
  IconUser,
  IconServer,
  IconRefresh,
  IconClock,
} from '@douyinfe/semi-icons';
import { API, showError } from '../../helpers';

const { Text, Title } = Typography;

const SessionMonitorPanel = () => {
  const { t } = useTranslation();
  const [loading, setLoading] = useState(true);
  const [sessionData, setSessionData] = useState(null);
  const [lastUpdate, setLastUpdate] = useState(null);

  const fetchSessionData = async () => {
    setLoading(true);
    try {
      const res = await API.get('/api/admin/sessions/summary?top_users_limit=10&recent_sessions_limit=15');
      if (res.data.success) {
        setSessionData(res.data.data);
        setLastUpdate(new Date());
      } else {
        showError(res.data.message || t('获取会话数据失败'));
      }
    } catch (error) {
      showError(error.message || t('获取会话数据失败'));
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    fetchSessionData();
    // Auto refresh every 30 seconds
    const interval = setInterval(fetchSessionData, 30000);
    return () => clearInterval(interval);
  }, []);

  const userColumns = [
    {
      title: t('用户'),
      dataIndex: 'username',
      key: 'username',
      render: (text, record) => (
        <Space>
          <IconUser className="text-blue-500" />
          <Text>{text}</Text>
          <Tag size="small" color="blue">ID: {record.user_id}</Tag>
        </Space>
      ),
    },
    {
      title: t('活跃会话数'),
      dataIndex: 'session_count',
      key: 'session_count',
      render: (count) => (
        <Tag color={count > 5 ? 'red' : count > 2 ? 'orange' : 'green'}>
          {count}
        </Tag>
      ),
    },
  ];

  const sessionColumns = [
    {
      title: t('会话ID'),
      dataIndex: 'session_id',
      key: 'session_id',
      width: 200,
      render: (text) => (
        <Tooltip content={text}>
          <Text ellipsis={{ showTooltip: false }} style={{ width: 180 }}>
            {text}
          </Text>
        </Tooltip>
      ),
    },
    {
      title: t('用户'),
      dataIndex: 'user_id',
      key: 'user_id',
      width: 80,
      render: (id) => <Tag size="small">#{id}</Tag>,
    },
    {
      title: t('模型'),
      dataIndex: 'model',
      key: 'model',
      width: 150,
      render: (text) => (
        <Tooltip content={text}>
          <Tag color="blue" size="small">
            <Text ellipsis={{ showTooltip: false }} style={{ maxWidth: 120 }}>
              {text}
            </Text>
          </Tag>
        </Tooltip>
      ),
    },
    {
      title: t('渠道'),
      dataIndex: 'channel_id',
      key: 'channel_id',
      width: 80,
      render: (id) => <Tag size="small" color="cyan">#{id}</Tag>,
    },
    {
      title: t('剩余时间'),
      dataIndex: 'expires_in_seconds',
      key: 'expires_in_seconds',
      width: 100,
      render: (seconds) => {
        if (seconds <= 0) return <Tag color="red">{t('已过期')}</Tag>;
        const minutes = Math.floor(seconds / 60);
        return (
          <Tag color={minutes < 5 ? 'orange' : 'green'}>
            {minutes}m {seconds % 60}s
          </Tag>
        );
      },
    },
  ];

  const renderChannelStats = () => {
    if (!sessionData?.sessions_by_channel) return null;

    const entries = Object.entries(sessionData.sessions_by_channel);
    if (entries.length === 0) {
      return <Empty description={t('暂无渠道会话数据')} />;
    }

    const total = entries.reduce((acc, [, count]) => acc + count, 0);

    return (
      <div className="space-y-2">
        {entries.slice(0, 5).map(([channelId, count]) => (
          <div key={channelId} className="flex items-center gap-2">
            <Tag size="small" color="cyan">#{channelId}</Tag>
            <Progress
              percent={total > 0 ? Math.round((count / total) * 100) : 0}
              size="small"
              style={{ flex: 1 }}
            />
            <Text type="secondary" size="small">{count}</Text>
          </div>
        ))}
        {entries.length > 5 && (
          <Text type="tertiary" size="small">
            {t('还有 {{count}} 个渠道', { count: entries.length - 5 })}
          </Text>
        )}
      </div>
    );
  };

  if (loading && !sessionData) {
    return (
      <Card className="!rounded-2xl shadow-sm border-0 mb-4">
        <div className="flex justify-center items-center h-40">
          <Spin size="large" />
        </div>
      </Card>
    );
  }

  return (
    <Card className="!rounded-2xl shadow-sm border-0 mb-4">
      <div className="flex items-center justify-between mb-4">
        <div className="flex items-center">
          <IconServer className="text-purple-500 mr-2" size="large" />
          <Title heading={5} className="m-0">{t('会话监控')}</Title>
        </div>
        <Space>
          {lastUpdate && (
            <Text type="tertiary" size="small">
              <IconClock className="mr-1" />
              {lastUpdate.toLocaleTimeString()}
            </Text>
          )}
          <Button
            icon={<IconRefresh spin={loading} />}
            size="small"
            onClick={fetchSessionData}
            loading={loading}
          >
            {t('刷新')}
          </Button>
        </Space>
      </div>

      {!sessionData ? (
        <Empty description={t('暂无会话数据')} />
      ) : (
        <>
          {/* Stats Overview */}
          <Row gutter={16} className="mb-4">
            <Col span={8}>
              <Card
                className="!rounded-xl"
                bodyStyle={{ padding: '16px' }}
                style={{ background: 'linear-gradient(135deg, #667eea 0%, #764ba2 100%)' }}
              >
                <div className="text-white">
                  <Text className="text-white opacity-80">{t('总活跃会话')}</Text>
                  <Title heading={2} className="text-white m-0">
                    {sessionData.total_active_sessions || 0}
                  </Title>
                </div>
              </Card>
            </Col>
            <Col span={8}>
              <Card
                className="!rounded-xl"
                bodyStyle={{ padding: '16px' }}
                style={{ background: 'linear-gradient(135deg, #11998e 0%, #38ef7d 100%)' }}
              >
                <div className="text-white">
                  <Text className="text-white opacity-80">{t('活跃渠道数')}</Text>
                  <Title heading={2} className="text-white m-0">
                    {Object.keys(sessionData.sessions_by_channel || {}).length}
                  </Title>
                </div>
              </Card>
            </Col>
            <Col span={8}>
              <Card
                className="!rounded-xl"
                bodyStyle={{ padding: '16px' }}
                style={{ background: 'linear-gradient(135deg, #f093fb 0%, #f5576c 100%)' }}
              >
                <div className="text-white">
                  <Text className="text-white opacity-80">{t('活跃用户数')}</Text>
                  <Title heading={2} className="text-white m-0">
                    {sessionData.top_users_by_session?.length || 0}
                  </Title>
                </div>
              </Card>
            </Col>
          </Row>

          <Row gutter={16}>
            {/* Channel Distribution */}
            <Col span={12}>
              <Card className="!rounded-xl h-full" title={t('渠道会话分布')}>
                {renderChannelStats()}
              </Card>
            </Col>

            {/* Top Users */}
            <Col span={12}>
              <Card className="!rounded-xl h-full" title={t('会话数最多的用户')}>
                {sessionData.top_users_by_session?.length > 0 ? (
                  <Table
                    columns={userColumns}
                    dataSource={sessionData.top_users_by_session}
                    pagination={false}
                    size="small"
                    rowKey="user_id"
                  />
                ) : (
                  <Empty description={t('暂无用户会话数据')} />
                )}
              </Card>
            </Col>
          </Row>

          {/* Recent Sessions */}
          <Card className="!rounded-xl mt-4" title={t('最近会话')}>
            {sessionData.recent_sessions?.length > 0 ? (
              <Table
                columns={sessionColumns}
                dataSource={sessionData.recent_sessions}
                pagination={false}
                size="small"
                rowKey="session_id"
                scroll={{ y: 300 }}
              />
            ) : (
              <Empty description={t('暂无最近会话')} />
            )}
          </Card>
        </>
      )}
    </Card>
  );
};

export default SessionMonitorPanel;
