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

import { useState, useEffect, useCallback, useRef } from 'react';
import { useTranslation } from 'react-i18next';
import { API, showError, showSuccess } from '../../helpers';
import { PACKAGES_PAGE_SIZE } from '../../constants/packages.constants';
import { Modal } from '@douyinfe/semi-ui';

export const usePackagesData = () => {
  const { t } = useTranslation();

  // Basic states
  const [packages, setPackages] = useState([]);
  const [loading, setLoading] = useState(false);
  const [activePage, setActivePage] = useState(1);
  const [pageSize, setPageSize] = useState(PACKAGES_PAGE_SIZE);
  const [packageCount, setPackageCount] = useState(0);

  // UI states
  const [showEdit, setShowEdit] = useState(false);
  const [editingPackage, setEditingPackage] = useState({ id: undefined });
  const [showDeleteModal, setShowDeleteModal] = useState(false);
  const [deletingPackage, setDeletingPackage] = useState(null);

  // Filter states
  const [statusFilter, setStatusFilter] = useState('all');
  const [p2pGroupFilter, setP2pGroupFilter] = useState(0);
  const [searchKeyword, setSearchKeyword] = useState('');

  // Refs
  const requestCounter = useRef(0);

  // Load packages
  const loadPackages = useCallback(
    async (page = activePage) => {
      const reqId = ++requestCounter.current;
      setLoading(true);

      try {
        const params = new URLSearchParams({
          p: page,
          page_size: pageSize,
        });

        if (statusFilter !== 'all') {
          params.append('status', statusFilter);
        }

        if (p2pGroupFilter !== 0) {
          params.append('p2p_group_id', p2pGroupFilter);
        }

        if (searchKeyword) {
          params.append('keyword', searchKeyword);
        }

        const res = await API.get(`/api/packages?${params.toString()}`);

        // Check if this is the latest request
        if (reqId !== requestCounter.current) {
          return;
        }

        const { success, message, data } = res.data;
        if (success) {
          setPackages(data || []);
          setPackageCount(res.data.total || 0);
        } else {
          showError(message || t('加载套餐列表失败'));
        }
      } catch (error) {
        if (reqId === requestCounter.current) {
          showError(error.message || t('加载套餐列表失败'));
        }
      } finally {
        if (reqId === requestCounter.current) {
          setLoading(false);
        }
      }
    },
    [activePage, pageSize, statusFilter, p2pGroupFilter, searchKeyword, t],
  );

  // Initial load
  useEffect(() => {
    loadPackages(1);
  }, [statusFilter, p2pGroupFilter]);

  // Handle page change
  const handlePageChange = useCallback(
    (page) => {
      setActivePage(page);
      loadPackages(page);
    },
    [loadPackages],
  );

  // Handle search
  const handleSearch = useCallback(() => {
    setActivePage(1);
    loadPackages(1);
  }, [loadPackages]);

  // Handle create new package
  const handleCreate = useCallback(() => {
    setEditingPackage({ id: undefined });
    setShowEdit(true);
  }, []);

  // Handle edit package
  const handleEdit = useCallback((pkg) => {
    setEditingPackage(pkg);
    setShowEdit(true);
  }, []);

  // Handle save package (create or update)
  const handleSave = useCallback(
    async (values) => {
      try {
        let res;
        if (editingPackage.id) {
          // Update existing package
          res = await API.put(`/api/packages`, {
            ...values,
            id: editingPackage.id,
          });
        } else {
          // Create new package
          res = await API.post('/api/packages', values);
        }

        const { success, message } = res.data;
        if (success) {
          showSuccess(
            editingPackage.id
              ? t('套餐更新成功')
              : t('套餐创建成功'),
          );
          setShowEdit(false);
          loadPackages(activePage);
          return true;
        } else {
          showError(message);
          return false;
        }
      } catch (error) {
        showError(error.message);
        return false;
      }
    },
    [editingPackage.id, t, loadPackages, activePage],
  );

  // Handle delete confirmation
  const handleDeleteConfirm = useCallback((pkg) => {
    setDeletingPackage(pkg);
    setShowDeleteModal(true);
  }, []);

  // Handle delete package
  const handleDelete = useCallback(async () => {
    if (!deletingPackage) return;

    try {
      const res = await API.delete(`/api/packages`, {
        params: { id: deletingPackage.id },
      });

      const { success, message } = res.data;
      if (success) {
        showSuccess(t('套餐删除成功'));
        setShowDeleteModal(false);
        setDeletingPackage(null);

        // Reload current page or go back if empty
        if (packages.length === 1 && activePage > 1) {
          handlePageChange(activePage - 1);
        } else {
          loadPackages(activePage);
        }
      } else {
        showError(message);
      }
    } catch (error) {
      showError(error.message);
    }
  }, [deletingPackage, t, packages.length, activePage, loadPackages, handlePageChange]);

  // Handle status toggle
  const handleStatusToggle = useCallback(
    async (pkg) => {
      const newStatus = pkg.status === 1 ? 2 : 1;
      try {
        const res = await API.put('/api/packages', {
          id: pkg.id,
          status: newStatus,
        });

        const { success, message } = res.data;
        if (success) {
          showSuccess(
            newStatus === 1
              ? t('套餐已上架')
              : t('套餐已下架'),
          );
          loadPackages(activePage);
        } else {
          showError(message);
        }
      } catch (error) {
        showError(error.message);
      }
    },
    [t, loadPackages, activePage],
  );

  // Handle refresh
  const handleRefresh = useCallback(() => {
    loadPackages(activePage);
  }, [loadPackages, activePage]);

  return {
    // Data
    packages,
    packageCount,
    loading,

    // Pagination
    activePage,
    pageSize,
    setPageSize,

    // Filters
    statusFilter,
    setStatusFilter,
    p2pGroupFilter,
    setP2pGroupFilter,
    searchKeyword,
    setSearchKeyword,

    // Edit Modal
    showEdit,
    setShowEdit,
    editingPackage,

    // Delete Modal
    showDeleteModal,
    setShowDeleteModal,
    deletingPackage,

    // Handlers
    handlePageChange,
    handleSearch,
    handleCreate,
    handleEdit,
    handleSave,
    handleDeleteConfirm,
    handleDelete,
    handleStatusToggle,
    handleRefresh,
    loadPackages,
  };
};

export default usePackagesData;
