const { chromium } = require('playwright');
const fs = require('fs');
const path = require('path');

async function performLogin() {
  console.error('🔄 STEP 1: Reading environment variables...');
  const startTime = Date.now();

  const email = process.env.SRM_EMAIL;
  const password = process.env.SRM_PASSWORD;
  const timeout = parseInt(process.env.TIMEOUT_SECONDS || '30') * 1000;

  console.error(`📧 Email configured: ${email ? 'YES' : 'NO'}`);
  console.error(`🔑 Password configured: ${password ? 'YES' : 'NO'}`);
  console.error(`⏱️  Overall timeout: ${timeout/1000} seconds`);

  if (!email || !password) {
    console.error('❌ MISSING_CREDENTIALS: Email or password not provided');
    console.error(`⏱️  Step 1 duration: ${Date.now() - startTime}ms`);
    console.log(JSON.stringify({
      status: 'error',
      reason: 'MISSING_CREDENTIALS'
    }));
    process.exit(1);
  }

  console.error('✅ Environment variables validated');
  console.error(`⏱️  Step 1 duration: ${Date.now() - startTime}ms`);
  console.error('');

  let browser = null;
  let context = null;
  let page = null;

  try {
    console.error('🔄 STEP 2: Launching Playwright browser...');
    const step2Start = Date.now();

    // Launch browser
    browser = await chromium.launch({
      headless: true,
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

    console.error('✅ Browser launched successfully');
    console.error(`⏱️  Step 2 duration: ${Date.now() - step2Start}ms`);
    console.error('');

    console.error('🔄 STEP 3: Creating browser context and page...');
    const step3Start = Date.now();

    // Create context and page
    context = await browser.newContext({
      viewport: { width: 1280, height: 720 },
      userAgent: 'Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36'
    });

    page = await context.newPage();
    console.error('✅ Browser context and page created');
    console.error('📐 Viewport: 1280x720');
    console.error('🤖 User Agent set');
    console.error(`⏱️  Step 3 duration: ${Date.now() - step3Start}ms`);
    console.error('');

    console.error('🔄 STEP 4: Navigating to SRM Academia portal...');
    console.error('🌐 URL: https://academia.srmist.edu.in/');
    console.error('⏳ Wait condition: networkidle');
    const step4Start = Date.now();

    // Navigate to login page
    await page.goto('https://academia.srmist.edu.in/', {
      waitUntil: 'networkidle',
      timeout: timeout
    });

    console.error('✅ Page navigation completed');
    console.error(`📄 Current URL: ${page.url()}`);

    // Get page title and basic info
    const title = await page.title();
    console.error(`📋 Page title: "${title}"`);

    // Check if page loaded correctly
    if (!page.url().includes('academia.srmist.edu.in')) {
      console.error(`⏱️  Step 4 duration: ${Date.now() - step4Start}ms`);
      throw new Error('PAGE_LOAD_FAILED: Not on expected domain');
    }
    console.error(`⏱️  Step 4 duration: ${Date.now() - step4Start}ms`);
    console.error('');

    console.error('🔄 STEP 5: Capturing page HTML for analysis...');
    const step5Start = Date.now();

    // Get the full HTML content of the page
    const pageHTML = await page.content();
    console.error(`📄 Page HTML length: ${pageHTML.length} characters`);
    console.error('💾 Saving HTML to signup.html...');

    // Save to signup.html file
    const fs = require('fs');
    const path = require('path');
    fs.writeFileSync(path.join(__dirname, '..', 'signup.html'), pageHTML);
    console.error('✅ HTML saved to signup.html');
    console.error(`⏱️  Step 5 duration: ${Date.now() - step5Start}ms`);
    console.error('');

    console.error('🔄 STEP 6: Waiting for iframe to load...');
    console.error('🎯 Selector: iframe#signinFrame');
    console.error('⏳ No timeout limit - waiting indefinitely');
    const step6Start = Date.now();

    // Wait for iframe to be visible
    await page.waitForSelector('iframe#signinFrame');

    console.error('✅ Iframe found and loaded');
    console.error(`⏱️  Step 6 duration: ${Date.now() - step6Start}ms`);
    console.error('');

    console.error('🔄 STEP 7: Creating iframe locator...');
    console.error('🎯 Frame selector: iframe#signinFrame');
    const step7Start = Date.now();

    // Create frame locator for the signin iframe
    const signinFrame = page.frameLocator('iframe#signinFrame');

    console.error('✅ Iframe locator created');
    console.error(`⏱️  Step 7 duration: ${Date.now() - step7Start}ms`);
    console.error('');

    console.error('🔄 STEP 8: Looking for signin box inside iframe...');
    console.error('🎯 Selector: div.signin_box#signin_flow');
    console.error('⏳ No timeout limit - waiting indefinitely');
    const step8Start = Date.now();

    // Wait for signin box to be visible inside the iframe
    await signinFrame.locator('div.signin_box#signin_flow').waitFor();

    console.error('✅ Signin box found and visible inside iframe');
    console.error(`⏱️  Step 8 duration: ${Date.now() - step8Start}ms`);
    console.error('');

    console.error('🔄 STEP 9: Filling email address...');
    console.error('📧 Email input selector: #login_id (inside iframe)');
    const step9Start = Date.now();

    // Fill email inside iframe
    await signinFrame.locator('#login_id').fill(email);
    console.error(`✅ Email filled: ${email.replace(/./g, '*').substring(0, 3)}***@***`);
    console.error(`⏱️  Step 9 duration: ${Date.now() - step9Start}ms`);
    console.error('');

    console.error('🔄 STEP 10: Clicking Next button...');
    console.error('🔘 Button selector: button#nextbtn:has-text("Next") (inside iframe)');
    const step10Start = Date.now();

    // Click Next button inside iframe
    await signinFrame.locator('button#nextbtn:has-text("Next")').click();
    console.error('✅ Next button clicked');
    console.error(`⏱️  Step 10 duration: ${Date.now() - step10Start}ms`);
    console.error('');

    console.error('🔄 STEP 11: Waiting for password field to appear...');
    console.error('🔑 Password input selector: #password (inside iframe)');
    console.error('⏳ No timeout limit - waiting indefinitely');
    const step11Start = Date.now();

    // Wait for password field to appear inside iframe (page updates dynamically)
    await signinFrame.locator('#password').waitFor();

    console.error('✅ Password field appeared');
    console.error(`⏱️  Step 11 duration: ${Date.now() - step11Start}ms`);
    console.error('');

    console.error('🔄 STEP 12: Filling password...');
    console.error('🔑 Password input selector: #password (inside iframe)');
    const step12Start = Date.now();

    // Fill password inside iframe
    await signinFrame.locator('#password').fill(password);
    console.error(`✅ Password filled: ${'*'.repeat(password.length)}`);
    console.error(`⏱️  Step 12 duration: ${Date.now() - step12Start}ms`);
    console.error('');

    console.error('🔄 STEP 13: Clicking Sign In button...');
    console.error('🔘 Button selector: button#nextbtn (Sign In) (inside iframe)');
    const step13Start = Date.now();

    // Click Sign In button inside iframe (same button, now shows "Sign In")
    await signinFrame.locator('button#nextbtn').click();
    console.error('✅ Sign In button clicked');
    console.error(`⏱️  Step 13 duration: ${Date.now() - step13Start}ms`);
    console.error('');

    console.error('🔄 STEP 14: Waiting for login result...');
    console.error('⏳ Waiting for redirect to either:');
    console.error('   ✅ Success: /portal/academia-academic-services');
    console.error('   ⚠️  Rate limit: /announcement/signin-block');
    console.error('⏳ No timeout limit - waiting indefinitely');
    const stepCookiesStart = Date.now();

    // Wait for navigation or rate limit page
    try {
      await page.waitForURL(
        (url) => {
          // Success: portal home page
          if (url.href.includes('/portal/academia-academic-services')) {
            return true;
          }
          // Rate limit: blocked page
          if (url.href.includes('/accounts/p/') && url.href.includes('/announcement/signin-block')) {
            return true;
          }
          return false;
        }
      );

      console.error(`⏱️  Step 14 duration: ${Date.now() - stepCookiesStart}ms`);
      const finalUrl = page.url();
      console.error(`✅ Redirect detected to: ${finalUrl}`);

      if (finalUrl.includes('/portal/academia-academic-services')) {
        console.error('🎉 LOGIN SUCCESS: Redirected to portal home page');
      } else if (finalUrl.includes('/announcement/signin-block')) {
        console.error('⚠️  RATE LIMIT DETECTED: On rate limit page');
      }

    } catch (e) {
      console.error(`⏱️  Step 14 duration: ${Date.now() - stepCookiesStart}ms`);
      console.error('⚠️  No expected redirect detected');
      console.error('🔍 Checking current page URL...');

      // If we don't get expected redirects, check current URL
      const currentUrl = page.url();
      console.error(`📍 Current URL: ${currentUrl}`);

      if (currentUrl.includes('/announcement/signin-block')) {
        console.error('⚠️  RATE LIMIT: On rate limit page, clicking continue...');
        const rateLimitStart = Date.now();

        // Handle rate limit page
        await page.click('a#continue_button');
        console.error('✅ Continue button clicked');

        console.error('⏳ Waiting for redirect after rate limit bypass...');
        await page.waitForURL(
          (url) => url.href.includes('/portal/academia-academic-services')
        );

        console.error(`⏱️  Rate limit bypass duration: ${Date.now() - rateLimitStart}ms`);
        const afterRateLimitUrl = page.url();
        console.error(`✅ Redirected to: ${afterRateLimitUrl}`);

        if (afterRateLimitUrl.includes('/portal/academia-academic-services')) {
          console.error('🎉 RATE LIMIT BYPASSED: Successfully logged in');
        } else {
          throw new Error('RATE_LIMIT_BYPASS_FAILED');
        }

      } else if (!currentUrl.includes('/portal/academia-academic-services')) {
        console.error('❌ LOGIN FAILED: Not on expected success page');
        throw new Error('LOGIN_FAILED');
      } else {
        console.error('🎉 LOGIN SUCCESS: Already on portal page');
      }
    }

    console.error('');

    console.error('🔄 STEP 14: Extracting session cookies...');
    const cookieExtractStart = Date.now();

    // Extract all cookies
    const cookies = await context.cookies();
    console.error(`🍪 Found ${cookies.length} cookies`);

    const formattedCookies = cookies.map(cookie => ({
      name: cookie.name,
      value: cookie.value,
      domain: cookie.domain,
      path: cookie.path,
      httpOnly: cookie.httpOnly,
      secure: cookie.secure,
      expiry: cookie.expires
    }));

    console.error(`⏱️  Step 14 duration: ${Date.now() - cookieExtractStart}ms`);
    console.error('');
    console.error('🎉 LOGIN PROCESS COMPLETED SUCCESSFULLY');
    console.error(`⏱️  TOTAL DURATION: ${Date.now() - startTime}ms`);

    // Output success JSON
    console.log(JSON.stringify({
      status: 'success',
      timestamp: new Date().toISOString(),
      cookies: formattedCookies
    }));

    process.exit(0);

  } catch (error) {
    console.error('');
    console.error('❌ LOGIN PROCESS FAILED');
    console.error(`💥 Error: ${error.message}`);
    console.error(`⏱️  TOTAL DURATION BEFORE FAILURE: ${Date.now() - startTime}ms`);

    let reason = 'UNKNOWN_ERROR';

    if (error.message.includes('PAGE_LOAD_FAILED')) {
      reason = 'PAGE_LOAD_FAILED';
      console.error('🔍 DIAGNOSIS: Page did not load correctly (wrong domain)');
    } else if (error.message.includes('selector') || error.message.includes('not found')) {
      reason = 'SELECTOR_NOT_FOUND';
      console.error('🔍 DIAGNOSIS: Could not find expected HTML elements on page');
      console.error('   Possible causes:');
      console.error('   - SRM portal UI changed');
      console.error('   - Page took too long to load');
      console.error('   - Network connectivity issues');
    } else if (error.message.includes('timeout') || error.message.includes('Navigation timeout')) {
      reason = 'TIMEOUT';
      console.error('🔍 DIAGNOSIS: Operation timed out');
      console.error('   Possible causes:');
      console.error('   - Slow network connection');
      console.error('   - SRM portal slow/unresponsive');
      console.error('   - Browser performance issues');
    } else if (error.message.includes('LOGIN_FAILED')) {
      reason = 'LOGIN_FAILED';
      console.error('🔍 DIAGNOSIS: Login credentials rejected');
      console.error('   Possible causes:');
      console.error('   - Invalid email/password');
      console.error('   - Account locked/disabled');
      console.error('   - SRM portal authentication issues');
    } else if (error.message.includes('RATE_LIMIT_BYPASS_FAILED')) {
      reason = 'RATE_LIMIT_BLOCKED';
      console.error('🔍 DIAGNOSIS: Rate limit bypass failed');
      console.error('   Possible causes:');
      console.error('   - Too many login attempts');
      console.error('   - Rate limit page structure changed');
    } else if (error.message.includes('net::')) {
      reason = 'NETWORK_ERROR';
      console.error('🔍 DIAGNOSIS: Network connectivity issues');
      console.error('   Possible causes:');
      console.error('   - No internet connection');
      console.error('   - SRM portal unreachable');
      console.error('   - Firewall/proxy blocking');
    }

    console.error('');
    console.error('📤 Returning error response...');

    console.log(JSON.stringify({
      status: 'error',
      timestamp: new Date().toISOString(),
      reason: reason,
      details: error.message
    }));

    process.exit(1);

  } finally {
    // Clean up
    if (page) await page.close().catch(() => {});
    if (context) await context.close().catch(() => {});
    if (browser) await browser.close().catch(() => {});
  }
}

// Run the login
performLogin();
