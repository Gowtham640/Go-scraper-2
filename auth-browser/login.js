const { chromium } = require('playwright');
const http = require('http');

const PORT = parseInt(process.env.AUTH_SERVICE_PORT || '3001', 10);
const CONTEXT_COUNT = 3;
const pool = [];
let browser;

async function bootstrap() {
  browser = await chromium.launch({
    headless: false,
    args: [
      '--no-sandbox',
      '--disable-setuid-sandbox',
      '--disable-dev-shm-usage',
      '--disable-accelerated-2d-canvas',
      '--no-first-run',
      '--no-zygote',
      '--disable-gpu'
    ]
  });

  for (let i = 0; i < CONTEXT_COUNT; i++) {
    const context = await browser.newContext({
      viewport: { width: 1280, height: 720 },
      userAgent: 'Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36'
    });
    const page = await context.newPage();
    pool.push({ id: i, busy: false, context, page });
    console.log(`context-${i} ready`);
  }
}

function acquireContext() {
  return new Promise(resolve => {
    const attempt = () => {
      const slot = pool.find(entry => !entry.busy);
      if (slot) {
        slot.busy = true;
        return resolve(slot);
      }
      setTimeout(attempt, 100);
    };
    attempt();
  });
}

async function loginWithContext(entry, email, password) {
  const context = entry.context;
  const page = entry.page;
  const startTime = Date.now();
  const timeout = parseInt(process.env.TIMEOUT_SECONDS || '40', 10) * 1000;
  const emailToUse = email || process.env.SRM_EMAIL;
  const passwordToUse = password || process.env.SRM_PASSWORD;

  console.error('🔄 STEP 1: Reading environment variables...');
  console.error(`📧 Email configured: ${emailToUse ? 'YES' : 'NO'}`);
  console.error(`🔑 Password configured: ${passwordToUse ? 'YES' : 'NO'}`);
  console.error(`⏱️  Overall timeout: ${timeout / 1000} seconds`);

  if (!emailToUse || !passwordToUse) {
    console.error('❌ MISSING_CREDENTIALS: Email or password not provided');
    console.error(`⏱️  Step 1 duration: ${Date.now() - startTime}ms`);
    throw new Error('MISSING_CREDENTIALS');
  }

  console.error('✅ Environment variables validated');
  console.error(`⏱️  Step 1 duration: ${Date.now() - startTime}ms`);
  console.error('');

  console.error('🔄 STEP 2: Using existing browser context...');
  console.error(`⏱️  Step 2 duration: 0ms`);
  console.error('');

  console.error('🔄 STEP 3: Reusing existing page...');
  console.error(`⏱️  Step 3 duration: 0ms`);
  console.error('');

  console.error('🔄 STEP 4: Navigating to SRM Academia portal...');
  console.error('🌐 URL: https://academia.srmist.edu.in/');
  console.error('⏳ Wait condition: networkidle');
  const step4Start = Date.now();
  await page.goto('https://academia.srmist.edu.in/', {
    waitUntil: 'networkidle',
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
  const step5Start = Date.now();
  const pageHTML = await page.content();
  console.error(`📄 Page HTML length: ${pageHTML.length} characters`);
  console.error('💾 Saving HTML to signup.html...');
  const fs = require('fs');
  const path = require('path');
  fs.writeFileSync(path.join(__dirname, '..', 'signup.html'), pageHTML);
  console.error('✅ HTML saved to signup.html');
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
  await signinFrame.locator('#password').waitFor();
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
      await page.waitForLoadState('networkidle', { timeout: 10000 });
      await page.waitForTimeout(3000);

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
      await page.waitForLoadState('networkidle', { timeout: 10000 });
      await page.waitForTimeout(3000);

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
        if (currentUrlAfter.includes('/announcement/') || currentUrlAfter.includes('/preannouncement/')) {
          throw new Error(`Failed to redirect from session reminder page. Current URL: ${currentUrlAfter}`);
        }
        if (!currentUrlAfter.includes('/portal/academia-academic-services') && !currentUrlAfter.includes('#WELCOME')) {
          throw new Error(`Unexpected URL after session reminder skip: ${currentUrlAfter}`);
        }
      }

      console.error(`⏱️  Session reminder skip duration: ${Date.now() - sessionLimitStart}ms`);
      const afterSessionLimitUrl = page.url();
      console.error(`✅ Redirected to: ${afterSessionLimitUrl}`);
      if (!afterSessionLimitUrl.includes('/portal/academia-academic-services') && !afterSessionLimitUrl.includes('#WELCOME')) {
        throw new Error('SESSION_REMINDER_SKIP_FAILED');
      }

    } else if (!currentUrl.includes('/portal/academia-academic-services')) {
      console.error('❌ LOGIN FAILED: Not on expected success page');
      throw new Error('LOGIN_FAILED');
    } else {
      console.error('🎉 LOGIN SUCCESS: Already on portal page');
    }
  }

  console.error('');
  const currentUrl = page.url();
  if (!currentUrl.includes('/portal/academia-academic-services') && !currentUrl.includes('#WELCOME')) {
    console.error('❌ Cannot proceed to cookie extraction: Not on dashboard page');
    throw new Error('NOT_ON_DASHBOARD_PAGE');
  }

  console.error('🔄 STEP 15: Extracting session cookies...');
  const cookieExtractStart = Date.now();
  const cookies = await context.cookies();
  console.error(`🍪 Found ${cookies.length} cookies from browser context`);
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

  const formattedCookies = cookies.map(cookie => ({
    name: cookie.name,
    value: cookie.value,
    domain: cookie.domain,
    path: cookie.path,
    httpOnly: cookie.httpOnly,
    secure: cookie.secure,
    expiry: cookie.expires
  }));

  console.error(`⏱️  Step 16 duration: ${Date.now() - cookieExtractStart}ms`);
  console.error('');
  console.error('🎉 LOGIN PROCESS COMPLETED SUCCESSFULLY');
  console.error(`⏱️  TOTAL DURATION: ${Date.now() - startTime}ms`);

  return formattedCookies;
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

async function handleLogin(req, res) {
  try {
    let body = '';
    req.on('data', chunk => body += chunk);
    req.on('end', async () => {
      try {
        const { email, password } = JSON.parse(body);
        console.log(`Received login request for ${email}`);
        const entry = await acquireContext();
        console.log(`Assigned context-${entry.id} to ${email}`);
        try {
          const cookies = await loginWithContext(entry, email, password);
          res.writeHead(200, { 'Content-Type': 'application/json' });
          res.end(JSON.stringify({ status: 'success', cookies }));
        } catch (err) {
          console.error(`context-${entry.id} failure for ${email}: ${err.message}`);
          res.writeHead(500, { 'Content-Type': 'application/json' });
          res.end(JSON.stringify({ status: 'error', reason: err.message }));
        } finally {
          entry.busy = false;
        }
      } catch {
        res.writeHead(400, { 'Content-Type': 'application/json' });
        res.end(JSON.stringify({ status: 'error', reason: 'invalid payload' }));
      }
    });
  } catch {
    res.writeHead(500);
    res.end();
  }
}

async function startServer() {
  await bootstrap();
  const server = http.createServer((req, res) => {
    if (req.method === 'POST' && req.url === '/login') {
      return handleLogin(req, res);
    }
    res.writeHead(404);
    res.end();
  });
  server.listen(PORT, '127.0.0.1', () => {
    console.log(`Auth browser service listening on http://127.0.0.1:${PORT}`);
  });
}

startServer().catch(err => {
  console.error('Failed to start auth browser service', err);
  process.exit(1);
});
