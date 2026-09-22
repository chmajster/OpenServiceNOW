CREATE TABLE tenants (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    slug VARCHAR(100) NOT NULL,
    name VARCHAR(255) NOT NULL,
    status VARCHAR(32) NOT NULL DEFAULT 'ACTIVE',
    version BIGINT UNSIGNED NOT NULL DEFAULT 1,
    created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    updated_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6) ON UPDATE CURRENT_TIMESTAMP(6),
    PRIMARY KEY (id),
    UNIQUE KEY uq_tenants_slug (slug),
    KEY idx_tenants_status (status),
    CONSTRAINT chk_tenants_status CHECK (status IN ('ACTIVE', 'SUSPENDED', 'DISABLED'))
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE users (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    tenant_id BIGINT UNSIGNED NOT NULL,
    username VARCHAR(190) NOT NULL,
    email VARCHAR(320) NULL,
    display_name VARCHAR(255) NOT NULL,
    password_hash VARCHAR(255) NULL,
    auth_source VARCHAR(32) NOT NULL DEFAULT 'LOCAL',
    status VARCHAR(32) NOT NULL DEFAULT 'ACTIVE',
    version BIGINT UNSIGNED NOT NULL DEFAULT 1,
    created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    updated_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6) ON UPDATE CURRENT_TIMESTAMP(6),
    PRIMARY KEY (id),
    UNIQUE KEY uq_users_tenant_id (tenant_id, id),
    UNIQUE KEY uq_users_tenant_username (tenant_id, username),
    UNIQUE KEY uq_users_tenant_email (tenant_id, email),
    KEY idx_users_tenant_status (tenant_id, status),
    CONSTRAINT fk_users_tenant FOREIGN KEY (tenant_id) REFERENCES tenants(id) ON DELETE RESTRICT,
    CONSTRAINT chk_users_auth_source CHECK (auth_source IN ('LOCAL', 'LDAP', 'OIDC')),
    CONSTRAINT chk_users_status CHECK (status IN ('ACTIVE', 'LOCKED', 'DISABLED'))
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE groups (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    tenant_id BIGINT UNSIGNED NOT NULL,
    name VARCHAR(190) NOT NULL,
    description TEXT NULL,
    version BIGINT UNSIGNED NOT NULL DEFAULT 1,
    created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    updated_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6) ON UPDATE CURRENT_TIMESTAMP(6),
    PRIMARY KEY (id),
    UNIQUE KEY uq_groups_tenant_id (tenant_id, id),
    UNIQUE KEY uq_groups_tenant_name (tenant_id, name),
    CONSTRAINT fk_groups_tenant FOREIGN KEY (tenant_id) REFERENCES tenants(id) ON DELETE RESTRICT
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE roles (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    tenant_id BIGINT UNSIGNED NOT NULL,
    name VARCHAR(190) NOT NULL,
    description TEXT NULL,
    created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    updated_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6) ON UPDATE CURRENT_TIMESTAMP(6),
    PRIMARY KEY (id),
    UNIQUE KEY uq_roles_tenant_id (tenant_id, id),
    UNIQUE KEY uq_roles_tenant_name (tenant_id, name),
    CONSTRAINT fk_roles_tenant FOREIGN KEY (tenant_id) REFERENCES tenants(id) ON DELETE RESTRICT
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE permissions (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    name VARCHAR(190) NOT NULL,
    description TEXT NULL,
    created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    PRIMARY KEY (id),
    UNIQUE KEY uq_permissions_name (name)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE group_members (
    tenant_id BIGINT UNSIGNED NOT NULL,
    group_id BIGINT UNSIGNED NOT NULL,
    user_id BIGINT UNSIGNED NOT NULL,
    created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    PRIMARY KEY (group_id, user_id),
    KEY idx_group_members_tenant_user (tenant_id, user_id),
    CONSTRAINT fk_group_members_tenant FOREIGN KEY (tenant_id) REFERENCES tenants(id) ON DELETE RESTRICT,
    CONSTRAINT fk_group_members_group FOREIGN KEY (tenant_id, group_id) REFERENCES groups(tenant_id, id) ON DELETE CASCADE,
    CONSTRAINT fk_group_members_user FOREIGN KEY (tenant_id, user_id) REFERENCES users(tenant_id, id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE user_roles (
    tenant_id BIGINT UNSIGNED NOT NULL,
    user_id BIGINT UNSIGNED NOT NULL,
    role_id BIGINT UNSIGNED NOT NULL,
    created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    PRIMARY KEY (user_id, role_id),
    KEY idx_user_roles_tenant_role (tenant_id, role_id),
    CONSTRAINT fk_user_roles_tenant FOREIGN KEY (tenant_id) REFERENCES tenants(id) ON DELETE RESTRICT,
    CONSTRAINT fk_user_roles_user FOREIGN KEY (tenant_id, user_id) REFERENCES users(tenant_id, id) ON DELETE CASCADE,
    CONSTRAINT fk_user_roles_role FOREIGN KEY (tenant_id, role_id) REFERENCES roles(tenant_id, id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE role_permissions (
    role_id BIGINT UNSIGNED NOT NULL,
    permission_id BIGINT UNSIGNED NOT NULL,
    created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    PRIMARY KEY (role_id, permission_id),
    KEY idx_role_permissions_permission (permission_id, role_id),
    CONSTRAINT fk_role_permissions_role FOREIGN KEY (role_id) REFERENCES roles(id) ON DELETE CASCADE,
    CONSTRAINT fk_role_permissions_permission FOREIGN KEY (permission_id) REFERENCES permissions(id) ON DELEIÅM(§Å9%9ı%ππΩÅU1PÅ!IMPı’—ò·µà–Å=11Qı’—ò·µà—|¿‰¿¡}Ö•}ç§Ï()IQÅQ	1ÅÖ’ë•—}ïŸïπ—ÃÄ†(ÄÄÄÅ•êÅ	%%9PÅU9M%9Å9=PÅ9U10ÅUQ=}%9I59P∞(ÄÄÄÅ—ïπÖπ—}•êÅ	%%9PÅU9M%9Å9U10∞(ÄÄÄÅÖç—Ω…}•êÅ	%%9PÅU9M%9Å9U10∞(ÄÄÄÅÖç—•Ω∏ÅYI!H†ƒ‰¿§Å9=PÅ9U10∞(ÄÄÄÅΩâ©ïç—}—Â¡îÅYI!H†ƒ¿¿§Å9=PÅ9U10∞(ÄÄÄÅΩâ©ïç—}•êÅYI!H†ƒ‰¿§Å9=PÅ9U10∞(ÄÄÄÅΩ±ë}ŸÖ±’ïÃÅ)M=8Å9U10∞(ÄÄÄÅπï›}ŸÖ±’ïÃÅ)M=8Å9U10∞(ÄÄÄÅ…ï≈’ïÕ—}•êÅYI!H†ÿ–§Å9U10∞(ÄÄÄÅÕΩ’…çîÅYI!H†ÿ–§Å9=PÅ9U10ÅU1PÄùÖ¡¡±•çÖ—•Ω∏ú∞(ÄÄÄÅ•¿ÅYI	%9Id†ƒÿ§Å9U10∞(ÄÄÄÅç…ïÖ—ïë}Ö–ÅQQ%5†ÿ§Å9=PÅ9U10ÅU1PÅUII9Q}Q%5MQ5@†ÿ§∞(ÄÄÄÅAI%5IdÅ-dÄ°•ê§∞(ÄÄÄÅ-dÅ•ë·}Ö’ë•—}—ïπÖπ—}ç…ïÖ—ïêÄ°—ïπÖπ—}•ê∞Åç…ïÖ—ïë}Ö–§∞(ÄÄÄÅ-dÅ•ë·}Ö’ë•—}Ωâ©ïç–Ä°—ïπÖπ—}•ê∞ÅΩâ©ïç—}—Â¡î∞ÅΩâ©ïç—}•ê∞Åç…ïÖ—ïë}Ö–§∞(ÄÄÄÅ-dÅ•ë·}Ö’ë•—}Öç—Ω»Ä°—ïπÖπ—}•ê∞ÅÖç—Ω…}•ê∞Åç…ïÖ—ïë}Ö–§∞(ÄÄÄÅ=9MQI%9PÅô≠}Ö’ë•—}—ïπÖπ–Å=I%8Å-dÄ°—ïπÖπ—}•ê§ÅII9LÅ—ïπÖπ—Ã°•ê§Å=8Å1QÅIMQI%P∞(ÄÄÄÅ=9MQI%9PÅô≠}Ö’ë•—}Öç—Ω»Å=I%8Å-dÄ°—ïπÖπ—}•ê∞ÅÖç—Ω…}•ê§ÅII9LÅ’Õï…Ã°—ïπÖπ—}•ê∞Å•ê§Å=8Å1QÅIMQI%P∞(ÄÄÄÅ=9MQI%9PÅç°≠}Ö’ë•—}Öç—Ω…}—ïπÖπ–Å!,Ä°Öç—Ω…}•êÅ%LÅ9U10Å=HÅ—ïπÖπ—}•êÅ%LÅ9=PÅ9U10§(§Å9%9ı%ππΩÅU1PÅ!IMPı’—ò·µà–Å=11Qı’—ò·µà—|¿‰¿¡}Ö•}ç§Ï