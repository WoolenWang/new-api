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

import React from 'react';
import { Tag, Tooltip } from '@douyinfe/semi-ui';
import PropTypes from 'prop-types';
import { useTranslation } from 'react-i18next';
import { PRIORITY_RANGES } from '../../constants/packages.constants';

/**
 * 套餐优先级徽章组件
 * 根据优先级数值显示不同颜色和说明
 * @param {number} priority - 优先级数值 (1-21)
 * @param {boolean} showTooltip - 是否显示提示信息
 */
const PackagePriorityBadge = ({ priority, showTooltip = true }) => {
  const { t } = useTranslation();

  // 根据优先级确定颜色和类型
  const getPriorityConfig = () => {
    if (priority >= PRIORITY_RANGES.SYSTEM_LOW.min && priority <= PRIORITY_RANGES.SYSTEM_LOW.max) {
      return {
        color: 'blue',
        type: 'solid',
        label: t('系统低优先级 (1-10)'),
        description: t('系统低优先级 (1-10)'),
      };
    } else if (priority === PRIORITY_RANGES.P2P_GROUP.value) {
      return {
        color: 'green',
        type: 'solid',
        label: t('P2P分组套餐 (11)'),
        description: t('P2P分组套餐固定优先级为11'),
      };
    } else if (
      priority >= PRIORITY_RANGES.SYSTEM_HIGH.min &&
      priority <= PRIORITY_RANGES.SYSTEM_HIGH.max
    ) {
      return {
        color: 'red',
        type: 'solid',
        label: t('系统高优先级 (12-21)'),
        description: t('系统高优先级 (12-21)'),
      };
    } else {
      // 默认情况（不应该出现）
      return {
        color: 'grey',
        type: 'outline',
        label: t('未知优先级'),
        description: t('优先级必须在1-21之间'),
      };
    }
  };

  const config = getPriorityConfig();

  const badge = (
    <Tag
      color={config.color}
      type={config.type}
      size="default"
      className="font-medium"
    >
      {priority}
    </Tag>
  );

  if (showTooltip) {
    return (
      <Tooltip
        content={
          <div className="text-sm">
            <div className="font-semibold mb-1">{config.label}</div>
            <div className="text-gray-200">{config.description}</div>
            <div className="text-gray-300 mt-1">
              {t('数字越大优先级越高')}
            </div>
          </div>
        }
      >
        {badge}
      </Tooltip>
    );
  }

  return badge;
};

PackagePriorityBadge.propTypes = {
  priority: PropTypes.number.isRequired,
  showTooltip: PropTypes.bool,
};

export default PackagePriorityBadge;
