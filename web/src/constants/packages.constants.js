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

// 套餐状态
export const PACKAGE_STATUS = {
  AVAILABLE: 1, // 可用
  DELISTED: 2, // 下架
};

export const PACKAGE_STATUS_OPTIONS = [
  { value: 'all', label: '全部状态' },
  { value: 1, label: '可用', color: 'green' },
  { value: 2, label: '下架', color: 'grey' },
];

// 订阅状态
export const SUBSCRIPTION_STATUS = {
  INVENTORY: 'inventory', // 库存
  ACTIVE: 'active', // 生效中
  EXPIRED: 'expired', // 已过期
};

export const SUBSCRIPTION_STATUS_OPTIONS = [
  { value: 'inventory', label: '库存', color: 'blue' },
  { value: 'active', label: '生效中', color: 'green' },
  { value: 'expired', label: '已过期', color: 'grey' },
];

// 时长类型
export const DURATION_TYPES = {
  WEEK: 'week',
  MONTH: 'month',
  QUARTER: 'quarter',
  YEAR: 'year',
};

export const DURATION_TYPE_OPTIONS = [
  { value: 'week', label: '周' },
  { value: 'month', label: '月' },
  { value: 'quarter', label: '季' },
  { value: 'year', label: '年' },
];

// 优先级范围
export const PRIORITY_RANGES = {
  SYSTEM_LOW: { min: 1, max: 10, label: '系统低优先级 (1-10)', color: 'blue' },
  P2P_GROUP: { value: 11, label: 'P2P分组套餐 (11)', color: 'green' },
  SYSTEM_HIGH: { min: 12, max: 21, label: '系统高优先级 (12-21)', color: 'red' },
};

// 默认优先级
export const DEFAULT_PRIORITY = {
  SYSTEM: 15, // 系统管理员创建的全局套餐默认优先级
  P2P: 11, // P2P分组套餐固定优先级
};

// 时间窗口类型
export const WINDOW_TYPES = {
  RPM: 'rpm',
  HOURLY: 'hourly',
  FOUR_HOURLY: 'four_hourly',
  DAILY: 'daily',
  WEEKLY: 'weekly',
  MONTHLY: 'monthly',
};

// 表单默认值
export const PACKAGE_FORM_INIT_VALUES = {
  name: '',
  description: '',
  status: PACKAGE_STATUS.AVAILABLE,
  priority: DEFAULT_PRIORITY.SYSTEM,
  p2p_group_id: 0,
  quota: 0,
  duration_type: DURATION_TYPES.MONTH,
  duration: 1,
  rpm_limit: 0,
  hourly_limit: 0,
  four_hourly_limit: 0,
  daily_limit: 0,
  weekly_limit: 0,
  fallback_to_balance: true,
};

// 列表分页配置
export const PACKAGES_PAGE_SIZE = 10;

// 套餐创建者类型
export const CREATOR_TYPES = {
  SYSTEM: 0, // 系统管理员
  USER: 1, // 普通用户/P2P所有者
};

export default {
  PACKAGE_STATUS,
  PACKAGE_STATUS_OPTIONS,
  SUBSCRIPTION_STATUS,
  SUBSCRIPTION_STATUS_OPTIONS,
  DURATION_TYPES,
  DURATION_TYPE_OPTIONS,
  PRIORITY_RANGES,
  DEFAULT_PRIORITY,
  WINDOW_TYPES,
  PACKAGE_FORM_INIT_VALUES,
  PACKAGES_PAGE_SIZE,
  CREATOR_TYPES,
};
