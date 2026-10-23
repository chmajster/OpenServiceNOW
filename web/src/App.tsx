import {
  createBrowserRouter,
  createRoutesFromElements,
  RouterProvider,
  Outlet,
  Navigate,
  Route,
} from "react-router-dom";
import { AuthProvider } from "./auth/AuthContext";
import { RequireAnyPermission, RequireAuth, RequirePermission } from "./auth/RouteGuards";
import { AppShell } from "./layout/AppShell";
import { AuditPage } from "./pages/AuditPage";
import { LoginPage } from "./pages/LoginPage";
import { OverviewPage } from "./pages/OverviewPage";
import { RbacPage } from "./pages/RbacPage";
import { TenantPage } from "./pages/TenantPage";
import { UsersPage } from "./pages/UsersPage";
import { UserProfilePage } from "./pages/UserProfilePage";
import { SetupPage } from "./pages/SetupPage";
import { DiagnosticsPage } from "./pages/DiagnosticsPage";
import { AccountPage } from "./pages/AccountPage";
import { NotificationsPage } from "./pages/NotificationsPage";
import { NotificationAdminPage } from "./pages/NotificationAdminPage";
import { SlaAdminPage } from "./pages/SlaAdminPage";
import { lazy, Suspense } from "react";
import { PageState } from "./components/PageState";
const CatalogPage = lazy(() =>
  import("./pages/CatalogPage").then((m) => ({ default: m.CatalogPage })),
);
const CatalogAdminPage = lazy(() =>
  import("./pages/CatalogAdminPage").then((m) => ({
    default: m.CatalogAdminPage,
  })),
);
const RequestsPage = lazy(() =>
  import("./pages/RequestsPage").then((m) => ({ default: m.RequestsPage })),
);
const WorkflowPage = lazy(() =>
  import("./pages/WorkflowPage").then((m) => ({ default: m.WorkflowPage })),
);
const IncidentsPage = lazy(() =>
  import("./pages/IncidentsPage").then((m) => ({ default: m.IncidentsPage })),
);
const ApprovalsPage = lazy(() =>
  import("./pages/ApprovalsPage").then((m) => ({ default: m.ApprovalsPage })),
);
const AssetsPage = lazy(() =>
  import("./pages/AssetsPage").then((m) => ({ default: m.AssetsPage })),
);
const GlobalSearchPage = lazy(() =>
  import("./pages/GlobalSearchPage").then((m) => ({
    default: m.GlobalSearchPage,
  })),
);
const CMDBListPage = lazy(() =>
  import("./pages/CMDBPage").then((m) => ({ default: m.CMDBListPage })),
);
const CMDBCreatePage = lazy(() =>
  import("./pages/CMDBPage").then((m) => ({ default: m.CMDBCreatePage })),
);
const CMDBDetailPage = lazy(() =>
  import("./pages/CMDBPage").then((m) => ({ default: m.CMDBDetailPage })),
);
const CMDBImportsPage = lazy(() =>
  import("./pages/CMDBImportsPage").then((m) => ({ default: m.CMDBImportsPage })),
);
const CMDBImportDetailPage = lazy(() =>
  import("./pages/CMDBImportsPage").then((m) => ({ default: m.CMDBImportDetailPage })),
);

const router = createBrowserRouter(
  createRoutesFromElements(
    <Route
      element={
        <AuthProvider>
          <Suspense fallback={<PageState title="Ładowanie widoku…" />}>
            <Outlet />
          </Suspense>
        </AuthProvider>
      }
    >
      <Route path="/login" element={<LoginPage />} />
      <Route path="/setup" element={<SetupPage />} />
      <Route
        element={
          <RequireAuth>
            <AppShell />
          </RequireAuth>
        }
      >
        <Route index element={<OverviewPage />} />
        <Route path="account" element={<AccountPage />} />
        <Route path="search" element={<GlobalSearchPage />} />
        <Route path="assets" element={<AssetsPage />} />
        <Route path="assets/:assetID" element={<AssetsPage />} />
        <Route path="cmdb" element={<RequireAnyPermission permissions={["cmdb.read", "cmdb.read_own"]}><CMDBListPage /></RequireAnyPermission>} />
        <Route path="cmdb/new" element={<RequireAnyPermission permissions={["cmdb.read", "cmdb.read_own"]}><CMDBCreatePage /></RequireAnyPermission>} />
        <Route path="cmdb/imports" element={<RequirePermission permission="cmdb.import"><CMDBImportsPage /></RequirePermission>} />
        <Route path="cmdb/imports/:importID" element={<RequirePermission permission="cmdb.import"><CMDBImportDetailPage /></RequirePermission>} />
        <Route path="cmdb/:ciID" element={<RequireAnyPermission permissions={["cmdb.read", "cmdb.read_own"]}><CMDBDetailPage /></RequireAnyPermission>} />
        <Route path="catalog" element={<CatalogPage />} />
        <Route path="catalog/:itemID" element={<CatalogPage />} />
        <Route path="requests" element={<RequestsPage />} />
        <Route path="requests/:recordID" element={<RequestsPage />} />
        <Route path="approvals" element={<ApprovalsPage />} />
        <Route path="approvals/:executionID" element={<ApprovalsPage />} />
        <Route path="workflow" element={<WorkflowPage />} />
        <Route path="fulfillment" element={<RequestsPage tasks />} />
        <Route path="fulfillment/:recordID" element={<RequestsPage tasks />} />
        <Route
          path="catalog-admin"
          element={
            <RequirePermission permission="admin.catalog.manage">
              <CatalogAdminPage />
            </RequirePermission>
          }
        />
        <Route path="notifications" element={<NotificationsPage />} />
        <Route
          path="sla"
          element={
            <RequirePermission permission="admin.sla.manage">
              <SlaAdminPage />
            </RequirePermission>
          }
        />
        <Route
          path="notification-admin"
          element={
            <RequirePermission permission="admin.notifications.manage">
              <NotificationAdminPage />
            </RequirePermission>
          }
        />
        <Route path="incidents" element={<IncidentsPage />} />
        <Route path="incidents/:incidentID" element={<IncidentsPage />} />
        <Route
          path="diagnostics"
          element={
            <RequirePermission permission="admin.rbac.manage">
              <DiagnosticsPage />
            </RequirePermission>
          }
        />
        <Route
          path="tenant"
          element={
            <RequirePermission permission="tenant.read">
              <TenantPage />
            </RequirePermission>
          }
        />
        <Route
          path="users"
          element={
            <RequirePermission permission="admin.users.manage">
              <UsersPage />
            </RequirePermission>
          }
        />
        <Route
          path="users/:userID"
          element={
            <RequirePermission permission="admin.users.manage">
              <UserProfilePage />
            </RequirePermission>
          }
        />
        <Route
          path="rbac"
          element={
            <RequirePermission permission="admin.rbac.manage">
              <RbacPage />
            </RequirePermission>
          }
        />
        <Route
          path="audit"
          element={
            <RequirePermission permission="admin.audit.read">
              <AuditPage />
            </RequirePermission>
          }
        />
      </Route>
      <Route path="*" element={<Navigate to="/" replace />} />
    </Route>,
  ),
);
export function App() {
  return <RouterProvider router={router} />;
}
