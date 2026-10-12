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
import { AuditPage } from "./pages/AuditPage";
import { LoginPage } from "./pages/LoginPage";
import { OverviewPage } from "./pages/OverviewPage";
import { RbacPage } from "./pages/RbacPage";
import { TenantPage } from "./pages/TenantPage";
import { UsersPage } from "./pages/UsersPage";
import { SetupPage } from "./pages/SetupPage";
import { DiagnosticsPage } from "./pages/DiagnosticsPage";
import { AccountPage } from "./pages/AccountPage";
import { IncidentsPage } from "./pages/IncidentsPage";
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
