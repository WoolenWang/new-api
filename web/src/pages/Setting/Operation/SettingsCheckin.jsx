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

import React, { useEffect, useState, useRef } from 'react';
import { Button, Col, Form, Row, Spin } from '@douyinfe/semi-ui';
import { useTranslation } from 'react-i18next';
import {
  compareObjects,
  API,
  showError,
  showSuccess,
  showWarning,
} from '../../../helpers';

export default function SettingsCheckin(props) {
  const { t } = useTranslation();
  const [loading, setLoading] = useState(false);
  const [inputs, setInputs] = useState({
    'checkin_setting.enabled': true,
    'checkin_setting.daily_quota': 1000,
    'checkin_setting.streak_bonus_quota': 5000,
    'checkin_setting.streak_days': 7,
  });
  const refForm = useRef();
  const [inputsRow, setInputsRow] = useState(inputs);

  function onSubmit() {
    const updateArray = compareObjects(inputs, inputsRow);
    if (!updateArray.length) return showWarning(t('你似乎并没有修改什么'));
    const requestQueue = updateArray.map((item) => {
      let value = '';
      if (typeof inputs[item.key] === 'boolean') {
        value = String(inputs[item.key]);
      } else {
        value = String(inputs[item.key]);
      }
      return API.put('/api/option/', {
        key: item.key,
        value,
      });
    });
    setLoading(true);
    Promise.all(requestQueue)
      .then((res) => {
        if (requestQueue.length === 1) {
          if (res.includes(undefined)) return;
        } else if (requestQueue.length > 1) {
          if (res.includes(undefined))
            return showError(t('部分保存失败，请重试'));
        }
        showSuccess(t('保存成功'));
        props.refresh();
      })
      .catch(() => {
        showError(t('保存失败，请重试'));
      })
      .finally(() => {
        setLoading(false);
      });
  }

  useEffect(() => {
    const currentInputs = {};
    for (let key in props.options) {
      if (Object.keys(inputs).includes(key)) {
        // Convert string values to proper types
        let value = props.options[key];
        if (key === 'checkin_setting.enabled') {
          value = value === 'true' || value === true;
        } else if (
          key === 'checkin_setting.daily_quota' ||
          key === 'checkin_setting.streak_bonus_quota' ||
          key === 'checkin_setting.streak_days'
        ) {
          value = parseInt(value, 10) || 0;
        }
        currentInputs[key] = value;
      }
    }
    // Set defaults if not present
    if (currentInputs['checkin_setting.enabled'] === undefined) {
      currentInputs['checkin_setting.enabled'] = true;
    }
    if (!currentInputs['checkin_setting.daily_quota']) {
      currentInputs['checkin_setting.daily_quota'] = 1000;
    }
    if (!currentInputs['checkin_setting.streak_bonus_quota']) {
      currentInputs['checkin_setting.streak_bonus_quota'] = 5000;
    }
    if (!currentInputs['checkin_setting.streak_days']) {
      currentInputs['checkin_setting.streak_days'] = 7;
    }
    setInputs(currentInputs);
    setInputsRow(structuredClone(currentInputs));
    refForm.current.setValues(currentInputs);
  }, [props.options]);

  return (
    <>
      <Spin spinning={loading}>
        <Form
          values={inputs}
          getFormApi={(formAPI) => (refForm.current = formAPI)}
          style={{ marginBottom: 15 }}
        >
          <Form.Section text={t('签到设置')}>
            <Row gutter={16}>
              <Col xs={24} sm={12} md={8} lg={8} xl={8}>
                <Form.Switch
                  label={t('启用签到功能')}
                  field={'checkin_setting.enabled'}
                  extraText={t('开启后，用户可以每日签到获得额度奖励')}
                  onChange={(value) =>
                    setInputs({
                      ...inputs,
                      'checkin_setting.enabled': value,
                    })
                  }
                />
              </Col>
            </Row>
            <Row gutter={16}>
              <Col xs={24} sm={12} md={8} lg={8} xl={8}>
                <Form.InputNumber
                  label={t('每日签到奖励额度')}
                  field={'checkin_setting.daily_quota'}
                  step={100}
                  min={0}
                  suffix={'Token'}
                  extraText={t('用户每日签到可获得的基础额度')}
                  placeholder={t('例如：1000')}
                  onChange={(value) =>
                    setInputs({
                      ...inputs,
                      'checkin_setting.daily_quota': value,
                    })
                  }
                />
              </Col>
              <Col xs={24} sm={12} md={8} lg={8} xl={8}>
                <Form.InputNumber
                  label={t('连续签到额外奖励')}
                  field={'checkin_setting.streak_bonus_quota'}
                  step={100}
                  min={0}
                  suffix={'Token'}
                  extraText={t('达到连续签到天数后的额外奖励')}
                  placeholder={t('例如：5000')}
                  onChange={(value) =>
                    setInputs({
                      ...inputs,
                      'checkin_setting.streak_bonus_quota': value,
                    })
                  }
                />
              </Col>
              <Col xs={24} sm={12} md={8} lg={8} xl={8}>
                <Form.InputNumber
                  label={t('连续签到奖励天数')}
                  field={'checkin_setting.streak_days'}
                  step={1}
                  min={1}
                  max={365}
                  suffix={t('天')}
                  extraText={t('连续签到多少天可获得额外奖励')}
                  placeholder={t('例如：7')}
                  onChange={(value) =>
                    setInputs({
                      ...inputs,
                      'checkin_setting.streak_days': value,
                    })
                  }
                />
              </Col>
            </Row>
            <Row>
              <Button size='default' onClick={onSubmit}>
                {t('保存签到设置')}
              </Button>
            </Row>
          </Form.Section>
        </Form>
      </Spin>
    </>
  );
}
