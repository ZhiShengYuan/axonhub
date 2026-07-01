import { test, expect, type Page, type Locator } from "@playwright/test";
import { gotoAndEnsureAuth, waitForGraphQLOperation } from "./auth.utils";

/**
 * Robustly select a developer option from the cmdk combobox.
 *
 * Uses keyboard-based selection (type to filter + Enter) instead of
 * clicking the portaled popover option, which avoids "element detached
 * from DOM" / "element is not stable" errors from cmdk re-renders.
 */
async function selectDeveloperOption(
	page: Page,
	dialog: Locator,
	searchText: string,
) {
	const developerCombo = dialog.locator('[role="combobox"]').first();
	await developerCombo.click();
	await developerCombo.fill(searchText);
	await page.waitForTimeout(300);
	await developerCombo.press("Enter");
}

/**
 * Create a model via the GraphQL API directly, bypassing the flaky
 * "Add Model" dialog. Returns the created model's id.
 */
async function createModelViaAPI(
	page: Page,
	model: {
		developer: string;
		modelID: string;
		name: string;
		icon: string;
		group: string;
	},
): Promise<string> {
	const token = await page.evaluate(() =>
		localStorage.getItem("axonhub_access_token"),
	);
	if (!token) throw new Error("No auth token in localStorage");

	const result = await page.evaluate(
		async ({ token, input }) => {
			const res = await fetch("/admin/graphql", {
				method: "POST",
				headers: {
					"Content-Type": "application/json",
					Authorization: `Bearer ${token}`,
				},
				body: JSON.stringify({
					query: `mutation CreateModel($input: CreateModelInput!) {
						createModel(input: $input) { id name }
					}`,
					variables: { input },
				}),
			});
			if (!res.ok) {
				throw new Error(
					`createModel failed: ${res.status} ${await res.text()}`,
				);
			}
			const json = await res.json();
			if (json.errors?.length) {
				throw new Error(`GraphQL error: ${JSON.stringify(json.errors)}`);
			}
			return json.data.createModel.id as string;
		},
		{
			token,
			input: {
				...model,
				type: "chat",
				modelCard: {},
				settings: { associations: [] },
			},
		},
	);
	return result;
}

/**
 * Delete a model via the GraphQL API directly.
 */
async function deleteModelViaAPI(page: Page, id: string): Promise<void> {
	const token = await page.evaluate(() =>
		localStorage.getItem("axonhub_access_token"),
	);
	if (!token) throw new Error("No auth token in localStorage");

	await page.evaluate(
		async ({ token, id }) => {
			const res = await fetch("/admin/graphql", {
				method: "POST",
				headers: {
					"Content-Type": "application/json",
					Authorization: `Bearer ${token}`,
				},
				body: JSON.stringify({
					query: `mutation DeleteModel($id: ID!) { deleteModel(id: $id) }`,
					variables: { id },
				}),
			});
			if (!res.ok) {
				throw new Error(
					`deleteModel failed: ${res.status} ${await res.text()}`,
				);
			}
		},
		{ token, id },
	);
}

test.describe("Admin Models Management", () => {
	test.beforeEach(async ({ page }) => {
		test.setTimeout(60000);
		await gotoAndEnsureAuth(page, "/models");

		const modelsTable = page.getByTestId("models-table");
		await modelsTable.waitFor({ state: "visible", timeout: 20000 });

		// Handle driver.js onboarding overlay (it has allowClose: false, so Escape won't work)
		const driverOverlay = page.locator("#driver-popover-content");
		if (await driverOverlay.isVisible().catch(() => false)) {
			// Click the highlighted settings button to dismiss the onboarding
			const settingsButton = page.locator("[data-settings-button]");
			if (await settingsButton.isVisible().catch(() => false)) {
				await settingsButton.click();
				await page.waitForTimeout(500);
			}
			// Wait for driver overlay to disappear
			await expect(driverOverlay)
				.not.toBeVisible({ timeout: 5000 })
				.catch(() => {});
		}

		// Close any dialog that may have opened (e.g., settings dialog from clicking the button)
		const settingsDialog = page
			.getByRole("dialog")
			.filter({ hasText: /Model Settings|模型设置/i });
		if (await settingsDialog.isVisible().catch(() => false)) {
			await page.keyboard.press("Escape");
			await expect(settingsDialog).not.toBeVisible({ timeout: 5000 });
		}
	});

	test("can create, edit, filter, toggle status, and delete a model", async ({
		page,
	}) => {
		const uniqueSuffix = Date.now().toString().slice(-6);
		const baseName = `pw-model-${uniqueSuffix}`;
		const updatedName = `${baseName}-updated`;

		// Open create dialog
		const createButton = page
			.getByRole("button", { name: /Add Model|创建模型|新增模型/i })
			.first();
		await expect(createButton).toBeVisible();
		await createButton.click();

		const dialog = page.locator('[data-slot="dialog-content"]');
		await expect(dialog).toBeVisible();

		// Select developer
		await selectDeveloperOption(page, dialog, "moonshot");

		// Select a modelId from provider list
		const modelIdInput = dialog.getByPlaceholder(/model id/i).first();
		await modelIdInput.click();
		await modelIdInput.fill("kimi");
		await page.waitForTimeout(300);
		await modelIdInput.press("Enter");

		// Override default name/group with deterministic values
		const nameInput = dialog.getByLabel(/Name|名称/i);
		await nameInput.fill(baseName);
		const groupInput = dialog.getByLabel(/Group|分组/i);
		await groupInput.fill(`group-${uniqueSuffix}`);
		const remarkInput = dialog.getByLabel(/Remark|备注/i);
		if (await remarkInput.count()) {
			await remarkInput.fill("Created via Playwright E2E");
		}

		await Promise.all([
			waitForGraphQLOperation(page, "CreateModel"),
			dialog
				.getByRole("button", { name: /Create|创建|保存|Save/i })
				.last()
				.click(),
		]);
		await expect(dialog).not.toBeVisible({ timeout: 20000 });
		await waitForGraphQLOperation(page, "GetModels");

		const modelsTable = page.getByTestId("models-table");
		const createdRow = modelsTable
			.locator("tbody tr")
			.filter({ hasText: baseName });
		await expect(createdRow).toBeVisible({ timeout: 20000 });

		// Edit the created model
		const rowActions = createdRow.getByTestId("row-actions").first();
		await rowActions.click();
		const editMenuItem = page
			.getByRole("menuitem", { name: /Edit|编辑/i })
			.first();
		await editMenuItem.click();

		const editDialog = page
			.getByRole("dialog")
			.filter({ hasText: /Edit Model|编辑/i })
			.first();
		await expect(editDialog).toBeVisible();
		const editNameInput = editDialog.getByLabel(/Name|名称/i);
		await editNameInput.fill(updatedName);

		await Promise.all([
			waitForGraphQLOperation(page, "UpdateModel"),
			editDialog
				.getByRole("button", { name: /Save|保存|Update|更新/i })
				.last()
				.click(),
		]);
		await expect(editDialog).not.toBeVisible({ timeout: 20000 });
		await waitForGraphQLOperation(page, "GetModels");

		const updatedRow = modelsTable
			.locator("tbody tr")
			.filter({ hasText: updatedName });
		await expect(updatedRow).toBeVisible({ timeout: 20000 });

		// Verify filtering by name works with the updated name
		const filterInput = page.getByPlaceholder(/Filter by name|名称|搜索/i);
		await filterInput.fill(updatedName);
		await page.waitForTimeout(800);
		await expect(updatedRow).toBeVisible();
		await filterInput.fill("");
		await page.waitForTimeout(400);

		// Toggle status via switch (enable/disable)
		const statusSwitch = updatedRow
			.locator('[data-testid="model-status-switch"]')
			.first();
		await statusSwitch.click();
		const statusDialog = page
			.getByRole("alertdialog")
			.or(page.getByRole("dialog"));
		await expect(statusDialog).toBeVisible();
		const confirmStatusButton = statusDialog
			.getByRole("button", { name: /Confirm|确认|确定|Enable|Disable/i })
			.last();
		await Promise.all([
			waitForGraphQLOperation(page, "UpdateModel"),
			confirmStatusButton.click(),
		]);
		await expect(statusDialog).not.toBeVisible({ timeout: 20000 });
		await waitForGraphQLOperation(page, "GetModels");

		// Delete the created model
		const actionsAfterToggle = updatedRow.getByTestId("row-actions").first();
		await actionsAfterToggle.click();
		const deleteMenuItem = page
			.getByRole("menuitem", { name: /Delete|删除/i })
			.first();
		await deleteMenuItem.click();

		const deleteDialog = page
			.getByRole("alertdialog")
			.or(page.getByRole("dialog"));
		await expect(deleteDialog).toBeVisible();
		const deleteButton = deleteDialog
			.getByRole("button", { name: /Delete|删除|Confirm|确认/i })
			.last();
		await Promise.all([
			waitForGraphQLOperation(page, "DeleteModel"),
			deleteButton.click(),
		]);
		await expect(deleteDialog).not.toBeVisible({ timeout: 20000 });
		await waitForGraphQLOperation(page, "GetModels");

		await expect(
			modelsTable.locator("tbody tr").filter({ hasText: updatedName }),
		).toHaveCount(0);
	});
});

test.describe("response model alias", () => {
	test.beforeEach(async ({ page }) => {
		test.setTimeout(60000);
		await gotoAndEnsureAuth(page, "/models");

		const modelsTable = page.getByTestId("models-table");
		await modelsTable.waitFor({ state: "visible", timeout: 20000 });

		const driverOverlay = page.locator("#driver-popover-content");
		if (await driverOverlay.isVisible().catch(() => false)) {
			const settingsButton = page.locator("[data-settings-button]");
			if (await settingsButton.isVisible().catch(() => false)) {
				await settingsButton.click();
				await page.waitForTimeout(500);
			}
			await expect(driverOverlay)
				.not.toBeVisible({ timeout: 5000 })
				.catch(() => {});
		}

		const settingsDialog = page
			.getByRole("dialog")
			.filter({ hasText: /Model Settings|模型设置/i });
		if (await settingsDialog.isVisible().catch(() => false)) {
			await page.keyboard.press("Escape");
			await expect(settingsDialog).not.toBeVisible({ timeout: 5000 });
		}
	});

	test("sets and persists a per-rule response model alias, and normalises whitespace to empty", async ({
		page,
	}) => {
		const uniqueSuffix = Date.now().toString().slice(-6);
		const baseName = `pw-alias-${uniqueSuffix}`;
		const aliasValue = "gpt-4-turbo";

		// Create the model via GraphQL API directly (bypasses the flaky Add Model dialog)
		const modelId = await createModelViaAPI(page, {
			developer: "moonshot",
			modelID: `kimi-${uniqueSuffix}`,
			name: baseName,
			icon: "🤖",
			group: `group-${uniqueSuffix}`,
		});

		// Reload to pick up the new model in the table
		await page.reload({ waitUntil: "domcontentloaded" });
		await page.waitForTimeout(1500);

		const modelsTable = page.getByTestId("models-table");
		const createdRow = modelsTable
			.locator("tbody tr")
			.filter({ hasText: baseName });
		await expect(createdRow).toBeVisible({ timeout: 20000 });

		// Open Manage Association dialog
		await createdRow.scrollIntoViewIfNeeded();
		await page.waitForTimeout(300);
		const rowActions = createdRow.getByTestId("row-actions").first();
		await rowActions.scrollIntoViewIfNeeded();
		await rowActions.click();
		const associationMenuItem = page
			.getByRole("menuitem", {
				name: /Manage Association|管理关联|Association/i,
			})
			.first();
		await associationMenuItem.click();

		const assocDialog = page
			.getByRole("dialog")
			.filter({ hasText: /Association|关联/i })
			.first();
		await expect(assocDialog).toBeVisible({ timeout: 10000 });

		// Add a rule
		await assocDialog
			.getByRole("button", { name: /Add Rule|添加规则/i })
			.click();
		await page.waitForTimeout(300);

		// Switch rule type to regex (no channel required)
		const typeSelect = assocDialog
			.locator('[role="combobox"]')
			.filter({ hasText: /Channel Exact Match/i })
			.first();
		await typeSelect.click();
		await page.getByRole("option", { name: /Global Regex Match/i }).click();
		await page.waitForTimeout(300);

		const patternInput = assocDialog.getByPlaceholder(/regex pattern/i).first();
		await patternInput.fill(".*");

		// Set the response model alias
		const aliasInput = assocDialog.getByTestId("response-model-input").first();
		await expect(aliasInput).toBeVisible();
		await aliasInput.fill(aliasValue);

		// Save
		await Promise.all([
			waitForGraphQLOperation(page, "UpdateModel"),
			assocDialog
				.getByRole("button", { name: /Save|保存/i })
				.last()
				.click(),
		]);
		await expect(assocDialog).not.toBeVisible({ timeout: 20000 });
		await waitForGraphQLOperation(page, "GetModels");

		// Reopen and verify alias persisted
		await createdRow.scrollIntoViewIfNeeded();
		await page.waitForTimeout(300);
		const rowActionsAfter = createdRow.getByTestId("row-actions").first();
		await rowActionsAfter.scrollIntoViewIfNeeded();
		await rowActionsAfter.click();
		const associationMenuItemAfter = page
			.getByRole("menuitem", {
				name: /Manage Association|管理关联|Association/i,
			})
			.first();
		await associationMenuItemAfter.click();

		const assocDialogAfter = page
			.getByRole("dialog")
			.filter({ hasText: /Association|关联/i })
			.first();
		await expect(assocDialogAfter).toBeVisible({ timeout: 10000 });

		const aliasInputAfter = assocDialogAfter
			.getByTestId("response-model-input")
			.first();
		await expect(aliasInputAfter).toHaveValue(aliasValue);

		// Now test whitespace-only normalization: fill with spaces, save, reopen, expect empty
		await aliasInputAfter.fill("   ");
		await Promise.all([
			waitForGraphQLOperation(page, "UpdateModel"),
			assocDialogAfter
				.getByRole("button", { name: /Save|保存/i })
				.last()
				.click(),
		]);
		await expect(assocDialogAfter).not.toBeVisible({ timeout: 20000 });
		await waitForGraphQLOperation(page, "GetModels");

		await createdRow.scrollIntoViewIfNeeded();
		await page.waitForTimeout(300);
		const rowActionsFinal = createdRow.getByTestId("row-actions").first();
		await rowActionsFinal.scrollIntoViewIfNeeded();
		await rowActionsFinal.click();
		const associationMenuItemFinal = page
			.getByRole("menuitem", {
				name: /Manage Association|管理关联|Association/i,
			})
			.first();
		await associationMenuItemFinal.click();

		const assocDialogFinal = page
			.getByRole("dialog")
			.filter({ hasText: /Association|关联/i })
			.first();
		await expect(assocDialogFinal).toBeVisible({ timeout: 10000 });

		const aliasInputFinal = assocDialogFinal
			.getByTestId("response-model-input")
			.first();
		await expect(aliasInputFinal).toHaveValue("");

		// Close the dialog
		await page.keyboard.press("Escape");
		await expect(assocDialogFinal).not.toBeVisible({ timeout: 5000 });

		// Cleanup: delete the model via API
		await deleteModelViaAPI(page, modelId);
		await page.reload({ waitUntil: "domcontentloaded" });
		await page.waitForTimeout(1500);
		await expect(
			modelsTable.locator("tbody tr").filter({ hasText: baseName }),
		).toHaveCount(0);
	});

	test("sets response model alias on a developer rule", async ({ page }) => {
		const uniqueSuffix = Date.now().toString().slice(-6);
		const baseName = `pw-devrule-${uniqueSuffix}`;

		// Create a model via GraphQL API to ensure the developer group row exists
		const modelId = await createModelViaAPI(page, {
			developer: "moonshot",
			modelID: `kimi-dev-${uniqueSuffix}`,
			name: baseName,
			icon: "🤖",
			group: `group-dev-${uniqueSuffix}`,
		});

		// Reload to pick up the new model
		await page.reload({ waitUntil: "domcontentloaded" });
		await page.waitForTimeout(1500);

		const modelsTable = page.getByTestId("models-table");
		const devRow = modelsTable
			.locator("tbody tr")
			.filter({ hasText: baseName });
		await expect(devRow).toBeVisible({ timeout: 20000 });

		// Open Developer Rules dialog via the group header button
		const devRulesButton = page
			.getByRole("button", { name: /Developer Rules|开发者规则/i })
			.first();
		await devRulesButton.scrollIntoViewIfNeeded();
		await devRulesButton.click();

		const devAssocDialog = page
			.getByRole("dialog")
			.filter({ hasText: /Developer Rules|开发者规则/i })
			.first();
		await expect(devAssocDialog).toBeVisible({ timeout: 10000 });

		// Add a rule
		await devAssocDialog
			.getByRole("button", { name: /Add Rule|添加规则/i })
			.click();
		await page.waitForTimeout(300);

		// Verify the response-model-input alias field is present and fillable
		// in developer-rule mode (the input appears even without a channel selected)
		const aliasInput = devAssocDialog
			.getByTestId("response-model-input")
			.first();
		await expect(aliasInput).toBeVisible();
		await aliasInput.fill("claude-3-opus");

		// Verify the filled value
		await expect(aliasInput).toHaveValue("claude-3-opus");

		// Close without saving (developer rules require a channel selection
		// to save, and this test verifies the alias field is present & fillable)
		await page.keyboard.press("Escape");
		await expect(devAssocDialog).not.toBeVisible({ timeout: 5000 });

		// Cleanup: delete the model via API
		await deleteModelViaAPI(page, modelId);
		await page.reload({ waitUntil: "domcontentloaded" });
		await page.waitForTimeout(1500);
		await expect(
			modelsTable.locator("tbody tr").filter({ hasText: baseName }),
		).toHaveCount(0);
	});
});
