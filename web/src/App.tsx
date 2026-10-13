import {
  createBrowserRouter,
  createRoutesFromElements,
  RouterProvider,
  Outlet,
  Navigate,
  Route,
} from "react-router-dom";
import { AuthProvider } from "./auth/AuthContext";
import { RequireAuth, RequirePermission } from "./auth/RouteGuards";
import { AppShell } from "./layout/AppShell";

import { LoginPage } from "./pages/LoginPage";

import { SetupPage } from "./pages/SetupPage";

import { lazy, Suspense } from "react";
import { PageState } from "./components/PageState";
const AuditPage = lazy(() =>
  import("./pages/AuditPage").then((m) => ({ default: m.AuditPage })),
);
const OverviewPage = lazy(() =>
  import("./pages/OverviewPage").then((m) => ({ default: m.OverviewPage })),
);
const RbacPage = lazy(() =>
  import("./pages/RbacPage").then((m) => ({ default: m.RbacPage })),
);
const TenantPage = lazy(() =>
  import("./pages/TenantPage").then((m) => ({ default: m.TenantPage })),
);
const UsersPage = lazy(() =>
  import("./pages/UsersPage").then((m) => ({ default: m.UsersPage })),
);
const DiagnosticsPage = lazy(() =>
  import("./pages/DiagnosticsPage").then((m) => ({
    default: m.DiagnosticsPage,
  })),
);
const AccountPage = lazy(() =>
  import("./pages/AccountPage").then((m) => ({ default: m.AccountPage })),
);
const IncidentsPage = lazy(() =>
  import("./pages/IncidentsPage").then((m) => ({ default: m.IncidentsPage })),
);
const NotificationsPage = lazy(() =>
  import("./pages/NotificationsPage").then((m) => ({
    default: m.NotificationsPage,
  })),
);
const NotificationAdminPage = lazy(() =>
  import("./pages/NotificationAdminPage").then((m) => ({
    default: m.NotificationAdminPage,
  })),
);
const SlaAdminPage = lazy(() =>
  import("./pages/SlaAdminPage").then((m) => ({ default: m.SlaAdminPage })),
);
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
const CmdbPage = lazy(() => import("./pages/CmdbPage").then(m=>({default:m.CmdbPage})));
const ApprovalsPage = lazy(() =>
  import("./pages/ApprovalsPage").then((m) => ({ default: m.ApprovalsPage })),
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
        <Route path="cmdb" element={<CmdbPage />} />
        <Route path="cmdb/:ciID" element={<CmdbPage />} />
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
