const { chromium } = require('playwright');
const http = require('http');
const { AsyncLocalStorage } = require('async_hooks');

const logContextStorage = new AsyncLocalStorage();
const consoleMethodsToWrap = new Set(['log', 'info', 'warn', 'error']);
const baseConsole = globalThis.console;
const proxiedConsole = new Proxy(baseConsole, {
  get(target, prop) {
    const value = target[prop];
    if (typeof value !== 'function' || !consoleMethodsToWrap.has(prop)) {
      return value;
    }
    return (...args) => {
      const store = logContextStorage.getStore();
      if (store?.email) {
        const segments = [];
        if (store.email) {
          segments.push(store.email);
        }
        if (store.contextId !== undefined && store.contextId !== null) {
          segments.push(`ctx-${store.contextId}`);
        }
        if (segments.length > 0) {
          return target[prop](`[${segments.join(',')}]`, ...args);
        }
      }
      return target[prop](...args);
    };
  }
});
globalThis.console = proxiedConsole;

const PORT = process.env.AUTH_SERVICE_PORT || 3001;
const WORKER_COUNT = 2;
const CONTEXTS_PER_WORKER = 3;
// Wall-clock budget for the whole login (HTTP handler rejects after this; does not force-close context).
const TASK_TIMEOUT_MS = 60000;

function assertPageOpen(page) {
  if (page.isClosed()) {
    throw new Error('Page closed unexpectedly');
  }
}

function formatCookiesForResponse(cookies) {
  return cookies.map(cookie => ({
    name: cookie.name,
    value: cookie.value,
    domain: cookie.domain,
    path: cookie.path,
    httpOnly: cookie.httpOnly,
    secure: cookie.secure,
    expiry: cookie.expires
  }));
}

const browserWorkers = Array.from({ length: WORKER_COUNT }, (_, index) => ({
  id: index,
  active: 0,
  slots: Array.from({ length: CONTEXTS_PER_WORKER }, () => false),
  slotContexts: Array.from({ length: CONTEXTS_PER_WORKER }, () => null),
  tasksHandled: 0,
  browser: null,
  launching: null
}));

async function launchBrowserForWorker(worker) {
  if (worker.launching) {
    return worker.launching;
  }

  worker.launching = chromium.launch({
    headless: true,
    args: ["--no-sandbox", "--disable-setuid-sandbox"]
  }).then(browser => {
    worker.browser = browser;
    worker.launching = null;
    browser.on('disconnected', () => {
      console.error(`worker-${worker.id} browser disconnected, forcing restart on next task`);
      worker.browser = null;
    });
    console.log(`worker-${worker.id} browser ready`);
    return browser;
  }).catch(err => {
    worker.launching = null;
    throw err;
  });

  return worker.launching;
}

async function ensureWorkerBrowser(worker) {
  if (worker.browser && worker.browser.isConnected()) {
    return worker.browser;
  }
  return launchBrowserForWorker(worker);
}

async function bootstrap() {
  await Promise.all(browserWorkers.map(worker => launchBrowserForWorker(worker)));
}

function acquireWorkerSlot() {
  return new Promise((resolve, reject) => {
    const startTime = Date.now();
    const attempt = () => {
      if (Date.now() - startTime >= 5000) {
        return reject(new Error('NO_AVAILABLE_WORKER'));
      }
      const worker = browserWorkers
        .filter(entry => entry.active < CONTEXTS_PER_WORKER)
        .sort((a, b) => a.active - b.active)[0];
      if (worker) {
        const slotId = worker.slots.findIndex(isBusy => !isBusy);
        if (slotId !== -1) {
          worker.slots[slotId] = true;
          worker.active += 1;
          return resolve({ worker, slotId });
        }
      }
      setTimeout(attempt, 100);
    };
    attempt();
  });
}

function parseBody(req) {
  return new Promise((resolve, reject) => {
    let body = '';
    req.on('data', chunk => {
      body += chunk;
    });
    req.on('end', () => {
      try {
        resolve(JSON.parse(body));
      } catch {
        reject(new Error('INVALID_JSON'));
      }
    });
    req.on('error', err => reject(err));
  });
}

function releaseWorkerSlot(worker, slotId) {
  if (slotId >= 0 && slotId < CONTEXTS_PER_WORKER) {
    worker.slots[slotId] = false;
    worker.slotContexts[slotId] = null;
  }
  worker.active = Math.max(0, worker.active - 1);
}

function withTimeout(promise, timeoutMs) {
  let timeoutRef;
  const timeoutPromise = new Promise((_, reject) => {
    timeoutRef = setTimeout(() => reject(new Error('LOGIN_TIMEOUT')), timeoutMs);
  });
  return Promise.race([promise, timeoutPromise]).finally(() => clearTimeout(timeoutRef));
}

async function loginWithContext(worker, contextSlotId, email, password) {
  return logContextStorage.run({ email, contextId: `${worker.id}-${contextSlotId}` }, async () => {
    const browser = await ensureWorkerBrowser(worker);
    let context = null;
    let isTimedOut = false;
    context = await browser.newContext({
      viewport: { width: 1280, height: 720 },
      userAgent: 'Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36'
    });
    worker.slotContexts[contextSlotId] = context;
    const page = await context.newPage();
    const startTime = Date.now();
    const timeout = TASK_TIMEOUT_MS;
    const emailToUse = email || process.env.SRM_EMAIL;
    const passwordToUse = password || process.env.SRM_PASSWORD;
    try {

      console.error('🔄 STEP 1: Reading environment variables...');
      console.error(`📧 Email configured: ${emailToUse ? 'YES' : 'NO'}`);
      console.error(`🔑 Password configured: ${passwordToUse ? 'YES' : 'NO'}`);
      console.error(`⏱️  Raw TIMEOUT_SECONDS: ${String(process.env.TIMEOUT_SECONDS ?? '(unset)')}`);
      console.error(`⏱️  Overall timeout: ${timeout / 1000} seconds`);

      if (!emailToUse || !passwordToUse) {
        console.error('❌ MISSING_CREDENTIALS: Email or password not provided');
        console.error(`⏱️  Step 1 duration: ${Date.now() - startTime}ms`);
        throw new Error('MISSING_CREDENTIALS');
      }

      console.error('✅ Environment variables validated');
      console.error(`⏱️  Step 1 duration: ${Date.now() - startTime}ms`);
      console.error('');

      console.error('🔄 STEP 2: Creating isolated browser context...');
      console.error(`⏱️  Step 2 duration: ${Date.now() - startTime}ms`);
      console.error('');

      console.error('🔄 STEP 3: Creating isolated page...');
      console.error(`⏱️  Step 3 duration: ${Date.now() - startTime}ms`);
      console.error('');

      console.error('🔄 STEP 4: Navigating to SRM Academia portal...');
      console.error('🌐 URL: https://academia.srmist.edu.in/');
      console.error('⏳ Wait condition: domcontentloaded');
      assertPageOpen(page);
      const step4Start = Date.now();
      await page.goto('https://academia.srmist.edu.in/', {
        waitUntil: 'domcontentloaded',
        timeout
      });
      console.error('✅ Page navigation completed');
      console.error(`📄 Current URL: ${page.url()}`);
      const title = await page.title();
      console.error(`📋 Page title: "${title}"`);
      if (!page.url().includes('academia.srmist.edu.in')) {
        console.error(`⏱️  Step 4 duration: ${Date.now() - step4Start}ms`);
        throw new Error('PAGE_LOAD_FAILED: Not on expected domain');
      }
      console.error(`⏱️  Step 4 duration: ${Date.now() - step4Start}ms`);
      console.error('');

      console.error('🔄 STEP 5: Capturing page HTML for analysis...');
      assertPageOpen(page);
      const step5Start = Date.now();
      const pageHTML = await page.content();
      console.error(`📄 Page HTML length: ${pageHTML.length} characters`);
      console.error(`⏱️  Step 5 duration: ${Date.now() - step5Start}ms`);
      console.error('');

      console.error('🔄 STEP 6: Waiting for iframe to load...');
      console.error('🎯 Selector: iframe#signinFrame');
      const step6Start = Date.now();
      await page.waitForSelector('iframe#signinFrame');
      console.error('✅ Iframe found and loaded');
      console.error(`⏱️  Step 6 duration: ${Date.now() - step6Start}ms`);
      console.error('');

      console.error('🔄 STEP 7: Creating iframe locator...');
      console.error('🎯 Frame selector: iframe#signinFrame');
      const step7Start = Date.now();
      const signinFrame = page.frameLocator('iframe#signinFrame');
      console.error('✅ Iframe locator created');
      console.error(`⏱️  Step 7 duration: ${Date.now() - step7Start}ms`);
      console.error('');

      console.error('🔄 STEP 8: Looking for signin box inside iframe...');
      console.error('🎯 Selector: div.signin_box#signin_flow');
      const step8Start = Date.now();
      await signinFrame.locator('div.signin_box#signin_flow').waitFor();
      console.error('✅ Signin box found and visible inside iframe');
      console.error(`⏱️  Step 8 duration: ${Date.now() - step8Start}ms`);
      console.error('');

      console.error('🔄 STEP 9: Filling email address...');
      console.error('📧 Email input selector: #login_id (inside iframe)');
      const step9Start = Date.now();
      await signinFrame.locator('#login_id').fill(emailToUse);
      const maskedEmail = emailToUse.replace(/./g, '*').substring(0, 3) + '***@***';
      console.error(`✅ Email filled: ${maskedEmail}`);
      console.error(`⏱️  Step 9 duration: ${Date.now() - step9Start}ms`);
      console.error('');

      console.error('🔄 STEP 10: Clicking Next button...');
      console.error('🔘 Button selector: button#nextbtn:has-text("Next") (inside iframe)');
      const step10Start = Date.now();
      await signinFrame.locator('button#nextbtn:has-text("Next")').click();
      console.error('✅ Next button clicked');
      console.error(`⏱️  Step 10 duration: ${Date.now() - step10Start}ms`);
      console.error('');

      console.error('🔄 STEP 11: Waiting for password field to appear...');
      console.error('🔑 Password input selector: #password (inside iframe)');
      const step11Start = Date.now();
      try {
        await signinFrame.locator('#password').waitFor();
      } catch (waitErr) {
        console.error('⚠️ Password field wait timed out, invoking stabilizer...');
        const stabilized = await stabilizePasswordField(page, signinFrame);
        if (!stabilized) {
          console.error('❌ Stabilizer could not recover password visibility');
          throw waitErr;
        }
        await signinFrame.locator('#password').waitFor({ timeout: 10000 });
      }
      console.error('✅ Password field appeared');
      console.error(`⏱️  Step 11 duration: ${Date.now() - step11Start}ms`);
      console.error('');

      console.error('🔄 STEP 12: Filling password...');
      console.error('🔑 Password input selector: #password (inside iframe)');
      const step12Start = Date.now();
      await signinFrame.locator('#password').fill(passwordToUse);
      console.error(`✅ Password filled: ${'*'.repeat(passwordToUse.length)}`);
      console.error(`⏱️  Step 12 duration: ${Date.now() - step12Start}ms`);
      console.error('');

      console.error('🔄 STEP 13: Clicking Sign In button...');
      console.error('🔘 Button selector: button#nextbtn (Sign In) (inside iframe)');
      const step13Start = Date.now();
      await signinFrame.locator('button#nextbtn').click();
      console.error('✅ Sign In button clicked');
      console.error(`⏱️  Step 13 duration: ${Date.now() - step13Start}ms`);
      console.error('');

      // Early success: already on dashboard after sign-in (skip long step 14 / stabilizer when possible).
      assertPageOpen(page);
      await page.waitForTimeout(1200);
      assertPageOpen(page);
      const earlyUrlAfterSignIn = page.url();
      if (earlyUrlAfterSignIn.includes('academia.srmist.edu.in') && isDashboardUrl(earlyUrlAfterSignIn)) {
        console.log('Early login success detected');
        let earlyCookies = await context.cookies();
        earlyCookies = await retryCookieExtraction(context, page, earlyCookies);
        console.error(`⏱️  TOTAL DURATION: ${Date.now() - startTime}ms`);
        return formatCookiesForResponse(earlyCookies);
      }

      console.error('🔄 STEP 14: Waiting for login result...');
      const stepCookiesStart = Date.now();

      await page.waitForTimeout(2000);
      const currentPageContent = await page.content();
      const hasSessionLimitContent = currentPageContent.includes('Maximum concurrent sessions limit exceeded') ||
        currentPageContent.includes('Terminate All Sessions');

      try {
        await page.waitForURL(
          (url) => {
            if (url.href.includes('/portal/academia-academic-services')) {
              return true;
            }
            if (url.href.includes('/accounts/p/') && url.href.includes('/announcement/signin-block')) {
              return true;
            }
            if (url.href.includes('/accounts/p/') && url.href.includes('/preannouncement/block-sessions')) {
              return true;
            }
            if (url.href.includes('/accounts/p/') && url.href.includes('/announcement/sessions-reminder')) {
              return true;
            }
            return false;
          },
          { timeout: 10000 }
        );

        console.error(`⏱️  Step 14 duration: ${Date.now() - stepCookiesStart}ms`);
        const finalUrl = page.url();
        console.error(`✅ Redirect detected to: ${finalUrl}`);

        if (finalUrl.includes('/portal/academia-academic-services') || finalUrl.hash.includes('#WELCOME')) {
          console.error('🎉 LOGIN SUCCESS: Redirected to portal home page');
        } else if (finalUrl.includes('/announcement/signin-block')) {
          console.error('⚠️  RATE LIMIT DETECTED: On rate limit page');
        } else if (finalUrl.includes('/preannouncement/block-sessions')) {
          console.error('🔄 SESSION LIMIT DETECTED: On session limit page');
        } else if (finalUrl.includes('/announcement/sessions-reminder')) {
          console.error('🔄 SESSION LIMIT DETECTED: On sessions reminder page');
        }

      } catch (e) {
        console.error(`⏱️  Step 14 duration: ${Date.now() - stepCookiesStart}ms`);
        console.error('⚠️  No expected redirect detected');
        console.error('🔍 Checking current page URL...');
        const currentUrl = page.url();
        console.error(`📍 Current URL: ${currentUrl}`);

        if (currentUrl.includes('/announcement/signin-block')) {
          console.error('⚠️  RATE LIMIT: On rate limit page, clicking continue...');
          const rateLimitStart = Date.now();
          await page.click('a#continue_button');
          console.error('✅ Continue button clicked');
          console.error('⏳ Waiting for redirect after rate limit bypass...');
          await page.waitForURL(
            (url) => url.href.includes('/portal/academia-academic-services')
          );
          console.error(`⏱️  Rate limit bypass duration: ${Date.now() - rateLimitStart}ms`);
          const afterRateLimitUrl = page.url();
          console.error(`✅ Redirected to: ${afterRateLimitUrl}`);
          if (!afterRateLimitUrl.includes('/portal/academia-academic-services')) {
            throw new Error('RATE_LIMIT_BYPASS_FAILED');
          }
        } else if (currentUrl.includes('/preannouncement/block-sessions') ||
          hasSessionLimitContent ||
          (currentUrl.includes('/accounts/p/') && currentPageContent.includes('block-sessions'))) {
          console.error('🔄 SESSION LIMIT: On session limit page, terminating other sessions...');
          const sessionLimitStart = Date.now();
          console.error('🔍 Looking for "Terminate all other sessions" button...');
          await page.waitForLoadState('domcontentloaded', { timeout: 10000 });
          await page.waitForTimeout(2000);

          try {
            console.error('🔍 URL before button click:', page.url());
            try {
              const buttonElement = await page.locator('a.blue_btn.continue_button#continue_button').first();
              await buttonElement.waitFor({ state: 'visible', timeout: 5000 });
              await buttonElement.click({ force: true });
              console.error('✅ Terminate All Sessions button clicked (direct locator with force)');
            } catch (directError) {
              await page.waitForSelector('text="Terminate All Sessions"', { timeout: 5000 });
              await page.click('text="Terminate All Sessions"', { force: true });
              console.error('✅ Terminate All Sessions button clicked (text with force)');
            }
            await page.waitForTimeout(1000);
            console.error('🔍 URL 1 second after button click:', page.url());
          } catch (textError) {
            console.error('⚠️ Primary button clicking failed, trying alternative strategies...');
            try {
              await page.evaluate(() => {
                const button = document.querySelector('a.blue_btn.continue_button#continue_button') ||
                  document.querySelector('a#continue_button') ||
                  Array.from(document.querySelectorAll('a')).find(a => a.textContent.trim() === 'Terminate All Sessions');
                if (button) {
                  button.click();
                  return true;
                }
                return false;
              });
              console.error('✅ Terminate All Sessions button clicked (via page.evaluate)');
              await page.waitForTimeout(1000);
              console.error('🔍 URL 1 second after button click:', page.url());
            } catch (evalError) {
              console.error('❌ All button clicking strategies failed');
              throw new Error('Could not find or click terminate sessions button');
            }
          }

          console.error('⏳ Waiting for redirect after session termination...');
          const urlLogger = setInterval(async () => {
            try {
              const currentUrl = page.url();
              console.error(`🔍 Current URL during wait: ${currentUrl}`);
            } catch (logError) {
              console.error('⚠️ Could not get current URL during wait');
            }
          }, 5000);

          try {
            await page.waitForURL(
              (url) => {
                console.error(`🔍 waitForURL checking: ${url.href}`);
                const isPortal = url.href.includes('/portal/academia-academic-services');
                const isWelcome = url.hash.includes('#WELCOME');
                const isNext = url.href.includes('/preannouncement/block-sessions/next');
                const isLogin = url.href.includes('/login') || url.href.includes('redirectLogin') || url.href.includes('signin');
                const isAccounts = url.href.includes('/accounts/');
                const isRedirectFromLogin = url.href.includes('redirectFromLogin');
                console.error(`🔍 Portal: ${isPortal}, Welcome: ${isWelcome}, Next: ${isNext}, Login: ${isLogin}, Accounts: ${isAccounts}, RedirectFromLogin: ${isRedirectFromLogin}`);
                return isPortal || isWelcome || isNext || isLogin || isAccounts || isRedirectFromLogin;
              },
              { timeout: 30000 }
            );
            console.error('✅ waitForURL completed successfully');
          } catch (redirectError) {
            const currentUrlAfter = page.url();
            console.error('⚠️ Expected redirect did not occur, checking current URL...');
            console.error(`📍 Current URL after button click: ${currentUrlAfter}`);
            const isSessionPage = currentUrlAfter.includes('/announcement/') || currentUrlAfter.includes('/preannouncement/');
            const isPortal = currentUrlAfter.includes('/portal/academia-academic-services');
            const isWelcome = currentUrlAfter.includes('#WELCOME');
            const isLogin = currentUrlAfter.includes('/login') || currentUrlAfter.includes('redirectLogin') || currentUrlAfter.includes('signin');
            const isAccounts = currentUrlAfter.includes('/accounts/');
            const isRedirectFromLogin = currentUrlAfter.includes('redirectFromLogin');
            console.error(`🔍 Catch block check - Session: ${isSessionPage}, Portal: ${isPortal}, Welcome: ${isWelcome}, Login: ${isLogin}, Accounts: ${isAccounts}, RedirectFromLogin: ${isRedirectFromLogin}`);
            if (isSessionPage) {
              throw new Error(`Failed to redirect from session limit page. Current URL: ${currentUrlAfter}`);
            }
            if (isLogin || isAccounts || isRedirectFromLogin) {
              try {
                await page.waitForURL(
                  (url) => url.href.includes('/portal/academia-academic-services') && !url.href.includes('redirectFromLogin'),
                  { timeout: 10000 }
                );
                console.error('✅ Reached final dashboard after redirect');
              } catch (dashboardError) {
                console.error('⚠️ Final dashboard redirect timeout, but redirect indicates success');
              }
            }
          } finally {
            clearInterval(urlLogger);
          }

          console.error(`⏱️  Session limit bypass duration: ${Date.now() - sessionLimitStart}ms`);
          const afterSessionLimitUrl = page.url();
          console.error(`✅ Redirected to: ${afterSessionLimitUrl}`);
          const isPortalUrl = afterSessionLimitUrl.includes('/portal/academia-academic-services');
          const isWelcomeUrl = afterSessionLimitUrl.includes('#WELCOME');
          const isLoginUrl = afterSessionLimitUrl.includes('/login') || afterSessionLimitUrl.includes('redirectLogin') || afterSessionLimitUrl.includes('signin');
          const isAccountsUrl = afterSessionLimitUrl.includes('/accounts/');
          const isRedirectFromLoginUrl = afterSessionLimitUrl.includes('redirectFromLogin');
          const isRootDomain = afterSessionLimitUrl === 'https://academia.srmist.edu.in/';
          console.error(`🔍 Success check - Portal: ${isPortalUrl}, Welcome: ${isWelcomeUrl}, Login: ${isLoginUrl}, Accounts: ${isAccountsUrl}, RedirectFromLogin: ${isRedirectFromLoginUrl}, RootDomain: ${isRootDomain}`);

          if (isPortalUrl || isWelcomeUrl || isLoginUrl || isAccountsUrl || isRedirectFromLoginUrl || isRootDomain) {
            console.error('🎉 SESSION LIMIT BYPASSED: Successfully redirected');
            if (isLoginUrl || isAccountsUrl || isRedirectFromLoginUrl || isRootDomain) {
              console.error('⏳ Waiting for final dashboard redirect...');
              try {
                await page.waitForURL(
                  (url) => url.href.includes('/portal/academia-academic-services') && !url.href.includes('redirectFromLogin'),
                  { timeout: 15000 }
                );
                console.error('✅ Reached final dashboard after redirect');
              } catch (finalRedirectError) {
                console.error('⚠️ Final dashboard redirect not detected, but redirect was successful');
              }
            }
          } else {
            throw new Error('SESSION_LIMIT_BYPASS_FAILED');
          }

        } else if (currentUrl.includes('/announcement/sessions-reminder') ||
          (hasSessionLimitContent && currentPageContent.includes('sessions-reminder'))) {
          console.error('🔄 SESSION LIMIT: On sessions reminder page, skipping for now...');
          const sessionLimitStart = Date.now();
          console.error('🔍 Looking for "Skip for now" link...');
          await page.waitForLoadState('domcontentloaded', { timeout: 10000 });
          await page.waitForTimeout(2000);

          try {
            console.error('🔍 URL before link click:', page.url());
            await page.click('a.remind_me_later');
            console.error('✅ Skip for now link clicked');
            await page.waitForTimeout(1000);
            console.error('🔍 URL 1 second after link click:', page.url());
          } catch (clickError) {
            console.error('❌ Could not click skip for now link');
            throw new Error('Could not find or click skip for now link');
          }

          console.error('⏳ Waiting for redirect after skipping session reminder...');
          try {
            await page.waitForURL(
              (url) => url.href.includes('/portal/academia-academic-services') ||
                url.href.includes('/announcement/sessions-reminder/next') ||
                url.hash.includes('#WELCOME'),
              { timeout: 30000 }
            );
          } catch (redirectError) {
            console.error('⚠️ Expected redirect did not occur, checking current URL...');
            const currentUrlAfter = page.url();
            console.error(`📍 Current URL after link click: ${currentUrlAfter}`);
            if (currentUrlAfter.includes('/announcement/signin-block')) {
              console.error('⚠️ Signin block detected after session reminder skip, attempting continue flow...');
              await handleSigninBlock(page);
            } else if (currentUrlAfter.includes('/announcement/') || currentUrlAfter.includes('/preannouncement/')) {
              throw new Error(`Failed to redirect from session reminder page. Current URL: ${currentUrlAfter}`);
            } else if (!currentUrlAfter.includes('/portal/academia-academic-services') && !currentUrlAfter.includes('#WELCOME')) {
              throw new Error(`Unexpected URL after session reminder skip: ${currentUrlAfter}`);
            }
          }

          console.error(`⏱️  Session reminder skip duration: ${Date.now() - sessionLimitStart}ms`);
          const afterSessionLimitUrl = page.url();
          console.error(`✅ Redirected to: ${afterSessionLimitUrl}`);
          if (!afterSessionLimitUrl.includes('/portal/academia-academic-services') &&
            !afterSessionLimitUrl.includes('#WELCOME') &&
            !isRootDomain(afterSessionLimitUrl)) {
            throw new Error('SESSION_REMINDER_SKIP_FAILED');
          }

        } else if (!isDashboardUrl(currentUrl) && !isRootDomain(currentUrl)) {
          console.error('❌ LOGIN FAILED: Not on expected success page');
          throw new Error('LOGIN_FAILED');
        } else {
      console.error('🎉 LOGIN SUCCESS: Already on portal page');
    }
  }

  console.error('');
  assertPageOpen(page);
  await stabilizeNavigation(page);
  const currentUrl = await ensureDashboardUrl(page);
      const currentHash = await page.evaluate(() => window.location.hash).catch(() => '');
      console.error(`🔍 Current location hash before cookie extraction: ${currentHash}`);
      if (!isDashboardUrl(currentUrl)) {
        console.error(`❌ Cannot proceed to cookie extraction: Not on dashboard page (url=${currentUrl})`);
        throw new Error('NOT_ON_DASHBOARD_PAGE');
      }

      console.error('🔄 STEP 15: Extracting session cookies...');
      const cookieExtractStart = Date.now();
      let cookies = await context.cookies();
      console.error(`🍪 Found ${cookies.length} cookies from browser context`);
      cookies = await retryCookieExtraction(context,page, cookies);
      console.error(`🍪 Final cookie count after retry validation: ${cookies.length}`);
      let pageCookies = [];
      try {
        pageCookies = await page.context().cookies();
        if (pageCookies.length !== cookies.length) {
          console.error(`🍪 Page context has ${pageCookies.length} cookies (different from browser context)`);
        }
      } catch (cookieError) {
        console.error('⚠️ Could not extract page cookies:', cookieError.message);
      }

      const cookieNames = cookies.map(cookie => cookie.name);
      const cookieDomains = [...new Set(cookies.map(cookie => cookie.domain))];
      console.error(`🍪 Cookie names: ${cookieNames.join(', ')}`);
      console.error(`🌐 Cookie domains: ${cookieDomains.join(', ')}`);

      const essentialCookies = ['JSESSIONID', 'iamcsr', 'zccpn', '_zcsr_tmp', 'stk', '__Secure-iamsdt_client_10002227248', '_iamadt_client_10002227248', '_iambdt_client_10002227248', 'wms-tkp-token_client_10002227248'];
      const foundEssential = essentialCookies.filter(name => cookieNames.includes(name));
      const missingEssential = essentialCookies.filter(name => !cookieNames.includes(name));
      console.error(`✅ Found essential cookies: ${foundEssential.join(', ')}`);
      if (missingEssential.length > 0) {
        console.error(`❌ Missing essential cookies: ${missingEssential.join(', ')}`);
      }

      const formattedCookies = formatCookiesForResponse(cookies);

      console.error(`⏱️  Step 16 duration: ${Date.now() - cookieExtractStart}ms`);
      console.error('');
      console.error('🎉 LOGIN PROCESS COMPLETED SUCCESSFULLY');
      console.error(`⏱️  TOTAL DURATION: ${Date.now() - startTime}ms`);

      return formattedCookies;
    } finally {
      try {
        isTimedOut = !context || worker.slotContexts[contextSlotId] === null;
        if (context) {
          await context.close();
        }
      } catch (closeErr) {
        console.error('⚠️ Failed to close context:', closeErr.message);
      } finally {
        if (!isTimedOut) {
          worker.slotContexts[contextSlotId] = null;
        }
      }
    }
  });
}

function isDashboardUrl(url) {
  if (!url) {
    return false;
  }
  return url.includes('/portal/academia-academic-services') ||
    url.includes('#WELCOME') ||
    isRootDomain(url);
}

async function ensureDashboardUrl(page) {
  let currentUrl = page.url();
  if (isDashboardUrl(currentUrl)) {
    console.error(`📍 Dashboard URL already detected: ${currentUrl}`);
    return currentUrl;
  }

  console.error('⚠️ Dashboard not detected yet, waiting for final landing...');
  try {
    await page.waitForURL(
      (url) => url.href.includes('/portal/academia-academic-services') || url.hash.includes('#WELCOME'),
      { timeout: 15000 }
    );
    console.error('✅ Dashboard redirect detected during wait');
  } catch (waitErr) {
    console.error('⚠️ Waiting for final dashboard redirect timed out:', waitErr.message);
  }

  currentUrl = page.url();
  console.error(`📍 URL after waiting: ${currentUrl}`);
  return currentUrl;
}

function isRootDomain(url) {
  return url === 'https://academia.srmist.edu.in/' || url === 'https://academia.srmist.edu.in';
}

async function handlePreannouncement(page) {
  const currentUrl = page.url();
  const onBlockSessions = currentUrl.includes('/preannouncement/block-sessions');
  const onSessionsReminder = currentUrl.includes('/announcement/sessions-reminder');
  if (!onBlockSessions && !onSessionsReminder) {
    return;
  }

  console.log('Session limit page detected:', currentUrl);

  if (onSessionsReminder) {
    console.log('Attempting "Skip for now"');
    if (await clickSelector(page, 'a.remind_me_later')) {
      await page.waitForTimeout(1500);
      console.log('Skip for now clicked');
      return;
    }
  }

  console.log('Attempting "Terminate all sessions"');
  if (await clickSelector(page, 'a.blue_btn.continue_button#continue_button, #continue_button, a#continue_button')) {
    await page.waitForTimeout(1500);
    console.log('Terminate All Sessions clicked');
    return;
  }

  console.error('Could not handle session limit page');
  throw new Error('PREANNOUNCEMENT_SKIP_FAILED');
}

async function clickSelector(page, selector) {
  try {
    await page.waitForSelector(selector, { timeout: 5000 });
    await page.click(selector, { force: true });
    return true;
  } catch {
    return false;
  }
}

async function handleSigninBlock(page, timeoutMs = 20000) {
  if (!page.url().includes('/announcement/signin-block')) {
    return false;
  }

  console.error('🔄 Handling signin block continuation page...');
  const start = Date.now();
  const selector = 'a#continue_button, a.blue_btn.continue_button#continue_button, #continue_button';

  const clicked = await clickSelector(page, selector);
  if (!clicked) {
    console.error('❌ Continue button missing on signin block page');
    throw new Error('SIGNIN_BLOCK_CONTINUE_NOT_FOUND');
  }

  console.error('⏳ Waiting for portal redirect after continue...');
  try {
    await page.waitForURL(
      (url) => url.href.includes('/portal/academia-academic-services') || url.hash.includes('#WELCOME'),
      { timeout: timeoutMs }
    );
    console.error(`✅ Signin block bypassed (${Date.now() - start}ms)`);
    return true;
  } catch (waitErr) {
    console.error('⚠️ Redirect after signin block continue did not happen in time');
    throw new Error('SIGNIN_BLOCK_REDIRECT_FAILED');
  }
}

async function stabilizeNavigation(page, maxCycles = 5, waitMs = 1000) {
  console.error('🛡️ Navigation stabilizer triggered');
  for (let attempt = 1; attempt <= maxCycles; attempt++) {
    const state = await detectState(page);
    console.error(`🧭 Stabilizer detected state: ${state.state} (${state.reason})`);
    if (state.state === 'portal') {
      const cookies = await page.context().cookies();
    
      if (hasCriticalCookies(cookies)) {
        return state;
      }
    
      console.error('⚠️ Portal reached but cookies not ready, waiting...');
      await page.waitForTimeout(1000);
      continue;
    }

    if (state.state === 'unknown') {
      console.error('⚠️ Unknown state, waiting for stabilization...');
      await Promise.race([
        page.waitForLoadState('domcontentloaded'),
        page.waitForTimeout(1000)
      ]);
      continue;
    }
    if (state.state === 'signin-block') {
      console.error('🧭 Stabilizer: Re-running signin block handler');
      await handleSigninBlock(page, 10000);
    } else if (state.state === 'session-limit' || state.state === 'session-reminder') {
      console.error('🧭 Stabilizer: Re-running preannouncement handler');
      await handlePreannouncement(page);
    } else {
      console.error('🧘 Stabilizer: No action mapped for this state');
      return state;
    }
    await page.waitForTimeout(waitMs);
  }
  const fallbackState = await detectState(page);
  console.error(`🧭 Stabilizer final state after retries: ${fallbackState.state}`);
  return fallbackState;
}

async function detectState(page) {
  const url = page.url();
  let continueVisible = false;
  let remindVisible = false;
  let terminateVisible = false;
  try {
    const [
      continueCount,
      remindCount,
      terminateCount
    ] = await Promise.all([
      page.locator('a#continue_button, a.blue_btn.continue_button#continue_button, #continue_button').count(),
      page.locator('a.remind_me_later').count(),
      page.getByText('Terminate All Sessions').count()
    ]);
    continueVisible = continueCount > 0;
    remindVisible = remindCount > 0;
    terminateVisible = terminateCount > 0;
  } catch (err) {
    console.error('⚠️ detectState DOM scan failed:', err.message);
  }

  if ((continueVisible && url.includes('/announcement/signin-block')) || url.includes('/announcement/signin-block')) {
    return { state: 'signin-block', reason: 'signin block matched', url };
  }
  if (remindVisible || url.includes('/announcement/sessions-reminder')) {
    return { state: 'session-reminder', reason: 'session reminder matched', url };
  }
  if (terminateVisible || url.includes('/preannouncement/block-sessions') || url.includes('/block-sessions')) {
    return { state: 'session-limit', reason: 'session limit matched', url };
  }
  if (url.includes('/portal/academia-academic-services') || url.includes('#WELCOME') || isRootDomain(url)) {
    return { state: 'portal', reason: 'dashboard URL matched', url };
  }

  return { state: 'unknown', reason: 'no known state detected', url };
}

async function describePasswordVisibility(locator) {
  try {
    return await locator.evaluate(el => {
      const rect = el.getBoundingClientRect();
      const style = window.getComputedStyle(el);
      const isVisible = style.display !== 'none' &&
        style.visibility !== 'hidden' &&
        parseFloat(style.opacity || '1') > 0 &&
        rect.width > 1 &&
        rect.height > 1 &&
        el.offsetParent !== null;
      return {
        isVisible,
        width: rect.width,
        height: rect.height,
        display: style.display,
        visibility: style.visibility,
        opacity: style.opacity,
        offsetParent: el.offsetParent !== null
      };
    });
  } catch (err) {
    console.error('⚠️ describePasswordVisibility failed:', err.message);
    return {
      isVisible: false,
      width: 0,
      height: 0,
      display: 'unknown',
      visibility: 'unknown',
      opacity: '0',
      offsetParent: false
    };
  }
}

async function stabilizePasswordField(page, frameLocator, attempts = 3, waitMs = 1500) {
  console.error('🛠 Password stabilizer invoked');
  const locator = frameLocator.locator('#password');
  for (let attempt = 1; attempt <= attempts; attempt++) {
    const state = await describePasswordVisibility(locator);
    console.error(`🧭 Stabilizer attempt ${attempt}: visible=${state.isVisible} width=${state.width} height=${state.height} display=${state.display}`);
    if (state.isVisible) {
      console.error('🧘 Password already visible, stabilizer complete');
      return true;
    }
    try {
      await locator.evaluate(el => {
        const container = document.getElementById('password_container') || el.closest('.getpassword');
        if (container) {
          container.style.display = 'block';
          container.style.visibility = 'visible';
          container.style.opacity = '1';
          container.style.height = 'auto';
          container.style.minHeight = '44px';
        }
        if (el.parentElement) {
          el.parentElement.style.display = 'block';
          el.parentElement.style.opacity = '1';
        }
        el.style.display = 'block';
        el.style.visibility = 'visible';
        el.style.opacity = '1';
        el.style.width = '100%';
        el.style.height = '40px';
        el.style.transform = 'none';
        el.scrollIntoView({ block: 'center', inline: 'center' });
      });
    } catch (evalErr) {
      console.error('⚠️ Password stabilizer adjustment failed:', evalErr.message);
    }
    await page.waitForTimeout(waitMs);
  }
  const finalState = await describePasswordVisibility(locator);
  console.error(`🧭 Stabilizer final state: visible=${finalState.isVisible} width=${finalState.width} height=${finalState.height}`);
  return finalState.isVisible;
}

function hasCriticalCookies(cookies) {
  const cookieNames = cookies.map(cookie => cookie.name);
  const hasIamcsr = cookieNames.includes('iamcsr');
  const hasWmsToken = cookieNames.some(name => /^wms-tkp-token_/i.test(name));
  return hasIamcsr && hasWmsToken;
}

async function retryCookieExtraction(context, page,initialCookies, maxRetries = 3) {
  let cookies = initialCookies;
  if (hasCriticalCookies(cookies)) {
    return cookies;
  }

  for (let attempt = 1; attempt <= maxRetries; attempt++) {
    console.error(`⚠️ Critical cookies missing, retry attempt ${attempt}/${maxRetries}`);
    await page.waitForLoadState('domcontentloaded');
    await delay(2000);
    cookies = await context.cookies();
    if (hasCriticalCookies(cookies)) {
      console.error('✅ Critical cookies recovered via retry');
      return cookies;
    }
  }

  console.error('❌ Critical cookies still missing after retries');
  return cookies;
}

function delay(ms) {
  return new Promise(resolve => setTimeout(resolve, ms));
}

async function resetPageForNextJob(page) {
  try {
    await page.evaluate(() => {
      localStorage.clear();
      sessionStorage.clear();
    });
  } catch (err) {
    console.error('⚠️ Failed to clear storage before next login:', err.message);
  }

  try {
    await page.context().clearCookies();
  } catch (err) {
    console.error('⚠️ Failed to clear cookies before next login:', err.message);
  }

  try {
    await page.goto('about:blank');
  } catch (err) {
    console.error('⚠️ Failed to reset page to about:blank:', err.message);
  }
}

async function handleLogin(req, res) {
  try {
    try {
      const { email, password } = await parseBody(req);
      await logContextStorage.run({ email }, async () => {
        console.log(`Received login request for ${email}`);
        const { worker, slotId: contextSlotId } = await acquireWorkerSlot();
        let isTimedOut = false;
        console.log(`Assigned worker-${worker.id} ctx-${contextSlotId} to ${email}`);
        try {
          const loginPromise = loginWithContext(worker, contextSlotId, email, password);
          let timeoutRef;
          const timeoutPromise = new Promise((_, reject) => {
            timeoutRef = setTimeout(() => {
              isTimedOut = true;
              console.warn('LOGIN TIMEOUT reached - returning failure without force closing context');
              reject(new Error('LOGIN_TIMEOUT'));
            }, TASK_TIMEOUT_MS);
          });
          const cookies = await Promise.race([loginPromise, timeoutPromise]).finally(() => clearTimeout(timeoutRef));
          res.writeHead(200, { 'Content-Type': 'application/json' });
          res.end(JSON.stringify({ status: 'success', cookies }));
        } catch (err) {
          console.error(`worker-${worker.id} ctx-${contextSlotId} failure for ${email}: ${err.message}`);
          res.writeHead(500, { 'Content-Type': 'application/json' });
          res.end(JSON.stringify({ status: 'error', reason: err.message }));
        } finally {
          releaseWorkerSlot(worker, contextSlotId);
          worker.tasksHandled += 1;
          if (worker.tasksHandled >= 50 && worker.active === 0 && worker.browser && worker.browser.isConnected()) {
            try {
              await worker.browser.close();
              worker.browser = null;
              worker.tasksHandled = 0;
            } catch (closeErr) {
              console.error(`worker-${worker.id} browser periodic restart failed:`, closeErr.message);
            }
          }
          if (!worker.browser || !worker.browser.isConnected()) {
            try {
              await launchBrowserForWorker(worker);
            } catch (restartErr) {
              console.error(`worker-${worker.id} browser restart failed:`, restartErr.message);
            }
          }
        }
      });
    } catch {
      res.writeHead(400, { 'Content-Type': 'application/json' });
      res.end(JSON.stringify({ status: 'error', reason: 'invalid payload' }));
    }
  } catch {
    res.writeHead(500);
    res.end();
  }
}

async function startServer() {
  await bootstrap();
  console.log(`Login task timeout: task_timeout_ms=${TASK_TIMEOUT_MS}`);
  const server = http.createServer((req, res) => {
    // Lightweight probe for Go /health and start-stack.ps1 wait loops
    if (req.method === 'GET' && (req.url === '/' || req.url === '/health')) {
      res.writeHead(200, { 'Content-Type': 'text/plain' });
      return res.end('ok');
    }
    if (req.method === 'POST' && req.url === '/login') {
      return handleLogin(req, res);
    }
    res.writeHead(404);
    res.end();
  });
  server.listen(PORT, '127.0.0.1', () => {
    console.log(`Auth browser service listening on http://0.0.0.0:${PORT}`);
  });
}

startServer().catch(err => {
  console.error('Failed to start auth browser service', err);
  process.exit(1);
});
