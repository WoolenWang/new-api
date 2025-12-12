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

import React, { useContext } from 'react';
import {
  Table,
  Button,
  Space,
  Tag,
  Tooltip,
  Modal,
  Form,
  InputNumber,
  Input,
  Select,
  Switch,
  Card,
  Row,
  Col,
} from '@douyinfe/semi-ui';
import { useTranslation } from 'react-i18next';
import { IconPlus, IconRefresh, IconDelete, IconEdit } from '@douyinfe/semi-icons';
import { usePackagesData } from '../../hooks/packages/usePackagesData';
import PackagePriorityBadge from '../common/PackagePriorityBadge';
import {
  PACKAGE_STATUS_OPTIONS,
  DURATION_TYPE_OPTIONS,
  PACKAGE_FORM_INIT_VALUES,
  DEFAULT_PRIORITY,
} from '../../constants/packages.constants';
import { UserContext } from '../../context/User';

const PackagesTable = () => {
  const { t } = useTranslation();
  const [userState] = useContext(UserContext);
  const isAdmin = userState.user?.role >= 10;

  const {
    packages,
    packageCount,
    loading,
    activePage,
    pageSize,
    setPageSize,
    statusFilter,
    setStatusFilter,
    searchKeyword,
    setSearchKeyword,
    showEdit,
    setShowEdit,
    editingPackage,
    showDeleteModal,
    setShowDeleteModal,
    deletingPackage,
    handlePageChange,
    handleSearch,
    handleCreate,
    handleEdit,
    handleSave,
    handleDeleteConfirm,
    handleDelete,
    handleStatusToggle,
    handleRefresh,
  } = usePackagesData();

  // Table columns definition
  const columns = [
    {
      title: 'ID',
      dataIndex: 'id',
      key: 'id',
      width: 80,
    },
    {
      title: t('套餐名称'),
      dataIndex: 'name',
      key: 'name',
      render: (text, record) => (
        <div>
          <div className="font-medium">{text}</div>
          {record.description && (
            <div className="text-gray-500 text-sm mt-1">
              {record.description}
            </div>
          )}
        </div>
      ),
    },
    {
      title: t('优先级'),
      dataIndex: 'priority',
      key: 'priority',
      width: 100,
      render: (priority) => <PackagePriorityBadge priority={priority} />,
    },
    {
      title: t('套餐类型'),
      dataIndex: 'duration_type',
      key: 'duration_type',
      width: 120,
      render: (type, record) => {
        const typeLabel = DURATION_TYPE_OPTIONS.find(opt => opt.value === type)?.label || type;
        return `${record.duration} ${typeLabel}`;
      },
    },
    {
      title: t('总额度'),
      dataIndex: 'quota',
      key: 'quota',
      width: 120,
      render: (quota) => {
        const quotaInM = (quota / 1000000).toFixed(1);
        return `${quotaInM}M`;
      },
    },
    {
      title: t('状态'),
      dataIndex: 'status',
      key: 'status',
      width: 100,
      render: (status) => {
        const config = PACKAGE_STATUS_OPTIONS.find(opt => opt.value === status);
        return (
          <Tag color={config?.color || 'grey'}>
            {config?.label || status}
          </Tag>
        );
      },
    },
    {
      title: t('归属分组'),
      dataIndex: 'p2p_group_id',
      key: 'p2p_group_id',
      width: 120,
      render: (groupId) => {
        return groupId === 0 ? (
          <Tag color="blue">{t('全局套餐')}</Tag>
        ) : (
          <Tag color="green">P2P #{groupId}</Tag>
        );
      },
    },
    {
      title: t('操作'),
      key: 'actions',
      width: 200,
      fixed: 'right',
      render: (_, record) => (
        <Space>
          <Button
            theme="borderless"
            type="primary"
            icon={<IconEdit />}
            onClick={() => handleEdit(record)}
          >
            {t('编辑')}
          </Button>
          <Button
            theme="borderless"
            type={record.status === 1 ? 'warning' : 'success'}
            onClick={() => handleStatusToggle(record)}
          >
            {record.status === 1 ? t('下架') : t('上架')}
          </Button>
          <Button
            theme="borderless"
            type="danger"
            icon={<IconDelete />}
            onClick={() => handleDeleteConfirm(record)}
          >
            {t('删除')}
          </Button>
        </Space>
      ),
    },
  ];

  // Form for creating/editing package
  const PackageEditForm = () => {
    const formApi = React.useRef();

    const handleSubmit = () => {
      formApi.current.validate().then((values) => {
        handleSave(values).then((success) => {
          if (success) {
            formApi.current.reset();
          }
        });
      });
    };

    return (
      <Modal
        title={editingPackage.id ? t('编辑套餐') : t('创建套餐')}
        visible={showEdit}
        onCancel={() => setShowEdit(false)}
        onOk={handleSubmit}
        width={800}
        style={{ maxHeight: '80vh', overflow: 'auto' }}
      >
        <Form
          getFormApi={(api) => (formApi.current = api)}
          initValues={editingPackage.id ? editingPackage : PACKAGE_FORM_INIT_VALUES}
          labelPosition="left"
          labelWidth="150px"
        >
          <Form.Input
            field="name"
            label={t('套餐名称')}
            rules={[{ required: true, message: t('套餐名称不能为空') }]}
            placeholder={t('例如：开发者月包')}
          />
          <Form.TextArea
            field="description"
            label={t('套餐描述')}
            placeholder={t('套餐的详细描述')}
            rows={3}
          />

          {isAdmin && (
            <>
              <Form.InputNumber
                field="priority"
                label={t('优先级')}
                rules={[
                  { required: true },
                  { type: 'number', min: 1, max: 21, message: t('优先级必须在1-21之间') },
                ]}
                min={1}
                max={21}
                initValue={DEFAULT_PRIORITY.SYSTEM}
                helpText={t('管理员可设置1-21任意优先级')}
              />
              <Form.InputNumber
                field="p2p_group_id"
                label={t('P2P分组ID')}
                min={0}
                initValue={0}
                helpText={t('选择归属的P2P分组，0表示全局套餐')}
              />
            </>
          )}

          <Form.InputNumber
            field="quota"
            label={t('总额度')}
            rules={[
              { required: true },
              { type: 'number', min: 1, message: t('总额度必须大于0') },
            ]}
            min={1}
            style={{ width: '100%' }}
            formatter={(value) => `${value}`.replace(/\B(?=(\d{3})+(?!\d))/g, ',')}
          />

          <Form.Select
            field="duration_type"
            label={t('时长类型')}
            rules={[{ required: true, message: t('时长类型不能为空') }]}
            optionList={DURATION_TYPE_OPTIONS}
          />

          <Form.InputNumber
            field="duration"
            label={t('时长数量')}
            rules={[
              { required: true },
              { type: 'number', min: 1, message: t('时长数量必须大于0') },
            ]}
            min={1}
            initValue={1}
          />

          <div className="text-gray-600 font-semibold mt-4 mb-2">
            {t('滑动窗口限额配置')}
          </div>

          <Form.InputNumber
            field="rpm_limit"
            label={t('RPM限制')}
            min={0}
            initValue={0}
            suffix={t('请求/分钟')}
            helpText={t('每分钟最多请求次数，0表示不限制')}
          />

          <Form.InputNumber
            field="hourly_limit"
            label={t('小时限额')}
            min={0}
            initValue={0}
            suffix="quota"
            helpText={t('每小时最多消耗的额度，0表示不限制')}
          />

          <Form.InputNumber
            field="four_hourly_limit"
            label={t('4小时限额')}
            min={0}
            initValue={0}
            suffix="quota"
            helpText={t('每4小时最多消耗的额度，0表示不限制')}
          />

          <Form.InputNumber
            field="daily_limit"
            label={t('每日限额')}
            min={0}
            initValue={0}
            suffix="quota"
            helpText={t('每日最多消耗的额度，0表示不限制')}
          />

          <Form.InputNumber
            field="weekly_limit"
            label={t('每周限额')}
            min={0}
            initValue={0}
            suffix="quota"
            helpText={t('每周最多消耗的额度，0表示不限制')}
          />

          <Form.Switch
            field="fallback_to_balance"
            label={t('Fallback到余额')}
            initValue={true}
            helpText={t('当套餐各项限额用尽时，是否允许自动切换到用户的常规余额')}
          />

          <Form.Select
            field="status"
            label={t('状态')}
            optionList={PACKAGE_STATUS_OPTIONS.filter(opt => opt.value !== 'all')}
            initValue={1}
          />
        </Form>
      </Modal>
    );
  };

  return (
    <div className="p-4">
      <Card>
        {/* Filters and Actions */}
        <div className="mb-4">
          <Row gutter={16}>
            <Col span={18}>
              <Space>
                <Select
                  value={statusFilter}
                  onChange={setStatusFilter}
                  optionList={PACKAGE_STATUS_OPTIONS}
                  style={{ width: 150 }}
                  placeholder={t('按状态过滤')}
                />
                <Input
                  value={searchKeyword}
                  onChange={setSearchKeyword}
                  onEnterPress={handleSearch}
                  placeholder={t('搜索套餐名称')}
                  style={{ width: 250 }}
                />
                <Button onClick={handleSearch}>{t('搜索')}</Button>
              </Space>
            </Col>
            <Col span={6} style={{ textAlign: 'right' }}>
              <Space>
                <Button
                  icon={<IconRefresh />}
                  onClick={handleRefresh}
                >
                  {t('刷新')}
                </Button>
                <Button
                  theme="solid"
                  type="primary"
                  icon={<IconPlus />}
                  onClick={handleCreate}
                >
                  {t('新建套餐')}
                </Button>
              </Space>
            </Col>
          </Row>
        </div>

        {/* Table */}
        <Table
          columns={columns}
          dataSource={packages}
          loading={loading}
          rowKey="id"
          pagination={{
            currentPage: activePage,
            pageSize: pageSize,
            total: packageCount,
            onPageChange: handlePageChange,
            onPageSizeChange: setPageSize,
            showSizeChanger: true,
            pageSizeOpts: [10, 20, 50, 100],
          }}
        />
      </Card>

      {/* Edit Modal */}
      {showEdit && <PackageEditForm />}

      {/* Delete Confirmation Modal */}
      <Modal
        title={t('确认删除')}
        visible={showDeleteModal}
        onCancel={() => setShowDeleteModal(false)}
        onOk={handleDelete}
        type="warning"
      >
        <p>
          {t('确认要删除套餐')} <strong>{deletingPackage?.name}</strong> {t('吗？')}
        </p>
        {deletingPackage?.active_subscriptions > 0 && (
          <div className="text-red-500 mt-2">
            {t('该套餐当前有')} {deletingPackage.active_subscriptions} {t('个活跃订阅')}
            <br />
            {t('请先下架套餐，等所有订阅过期后再删除')}
          </div>
        )}
      </Modal>
    </div>
  );
};

export default PackagesTable;
